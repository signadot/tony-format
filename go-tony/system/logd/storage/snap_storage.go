package storage

import (
	"fmt"
	"io"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/patches"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/snap"
)

// findSnapshotBaseReader is the base the next ROOT snapshot is built from: the most recent
// root snapshot at or below commit, opened over the whole document, and the commit the
// writes since it start at. Only root snapshots serve here, because what is layered over
// this base is whole-document entries (patchesSince), and a snapshot of a path would not
// align with them. A read's seek is findSubtreeBaseReader.
//
// Caller is responsible for closing the returned reader.
func (s *Storage) findSnapshotBaseReader(commit int64) (patches.EventReadCloser, int64, error) {
	snapSeg, ok := s.baselineSnapshotSegment(commit)

	// No snapshot found - start from empty (null state at commit 0)
	if !ok {
		return patches.NewEmptyEventReader(), 0, nil
	}

	// Do I/O without holding lock
	entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(snapSeg.LogFile), snapSeg.LogPosition, snapSeg.LogFileGeneration)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read snapshot entry: %w", err)
	}
	if entry.SnapPos == nil {
		return nil, 0, fmt.Errorf("snapshot entry missing SnapPos")
	}

	// Open reader at snapshot position to read the header
	snapReader, err := s.dLog.OpenReaderAt(dlog.LogFileID(snapSeg.LogFile), *entry.SnapPos, snapSeg.LogFileGeneration)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open snapshot reader: %w", err)
	}

	// Open the snapshot to parse header and get streaming event reader
	snapshot, err := snap.Open(snapReader)
	if err != nil {
		snapReader.Close()
		return nil, 0, fmt.Errorf("failed to open snapshot: %w", err)
	}

	// Get streaming event reader for the document (no in-memory materialization)
	eventReader, err := snapshot.ReadPathEventReader("")
	if err != nil {
		snapshot.Close()
		return nil, 0, fmt.Errorf("error creating event reader from snapshot: %w", err)
	}

	// Return wrapper that closes both the event reader and snapshot when done
	return &snapshotEventReadCloser{snapshot: snapshot, reader: eventReader}, snapSeg.StartCommit + 1, nil
}

// baselineSnapshotSegment answers the most recent ROOT snapshot at or below commit: the
// base the next root snapshot is built from, and the floor a per-path snapshot's commit
// must not go under (path_snapshot.go).
func (s *Storage) baselineSnapshotSegment(commit int64) (index.LogSegment, bool) {
	return s.index.SnapshotAtOrAbove("", commit)
}

// SwitchDLog switches the active log and creates a snapshot.
// Creates a baseline snapshot of the root; a scope is not snapshotted, its layer being
// its own patches (see the package doc).
// The snapshot is created for the current commit at the time of switching, in the log
// that has just become inactive, and when a compaction config is set, Compact runs on
// that log after it. This should be called periodically (e.g., based on log size or
// time) to enable snapshot-based read optimization and eventual compaction.
//
// Concurrency: the store's snapshot lock serializes the switch, with the root snapshot
// and the compaction after it, against a snapshot of a path. dlog also coordinates via
// per-file snapMu locks: SwitchActive blocks if a snapshot is in progress on the inactive
// log, and createSnapshot returns ErrSnapshotInProgress if called while another snapshot
// is running there.
func (s *Storage) SwitchDLog() error {
	// The inactive log has one writer at a time: this, with the root snapshot and the
	// compaction that rewrites the log, or a snapshot of a path (path_snapshot.go).
	s.snapMu.Lock()
	defer s.snapMu.Unlock()

	// Get current commit before switching
	commit, err := s.GetCurrentCommit()
	if err != nil {
		return fmt.Errorf("failed to get current commit: %w", err)
	}

	// Switch active log - blocks if snapshot in progress on inactive log
	if err := s.dLog.SwitchActive(); err != nil {
		return fmt.Errorf("failed to switch active log: %w", err)
	}

	// Create baseline snapshot. Scope snapshots are intentionally not created: a
	// materialized scope layer resolves !key away and is unsound to re-apply onto a
	// changed baseline. The scope layer is read as raw op-preserving patches instead
	// (see replayScopedAt). Bounded op-preserving compaction: 5hmq80f3h12krh1mbsn0.
	if err := s.createSnapshot(commit); err != nil {
		return fmt.Errorf("failed to create baseline snapshot: %w", err)
	}

	// Say what reads have been doing since the last snapshot. A store cannot be
	// asked afterwards, and from outside a narrow read and a wide one differ only in
	// how long they took -- which is exactly what is in doubt when a fix does not
	// show up downstream (ap8ddvp2h12krd43gdn0).
	if rs := s.ReadStats(); rs.Narrow+rs.WideRoot+rs.WideOperator+rs.WideAbsent > 0 {
		s.logger.Info("reads since start",
			"narrow", rs.Narrow, "wideRoot", rs.WideRoot,
			"wideOperator", rs.WideOperator, "wideAbsent", rs.WideAbsent,
			"wideKeyedOrIndexed", rs.WideNonField, "wideBadPath", rs.WideBadPath)
	}

	// Run compaction on the inactive log if configured
	if s.compactionConfig != nil {
		if err := s.Compact(s.compactionConfig); err != nil {
			// Log error but don't fail the switch - compaction is best-effort
			s.logger.Error("compaction failed", "error", err)
		}
	}

	return nil
}

// createSnapshot creates a baseline snapshot of the full state at the given commit.
// Writes snapshot events to the inactive log and adds an index entry.
//
// Scope snapshots are not created (a materialized scope layer cannot carry a claim); the
// scope layer is read from the scope's own patches instead (projectScope, and issue
// 5hmq80f3h12krh1mbsn0).
func (s *Storage) createSnapshot(commit int64) error {
	// Find most recent snapshot and get base event reader
	baseReader, startCommit, err := s.findSnapshotBaseReader(commit)
	if err != nil {
		return err
	}
	defer baseReader.Close()

	// The writes since that snapshot, one record at a time off the index's cursor. What
	// is held is the entries of one snapshot interval, which is the interval's to bound
	// and not history's.
	patchNodes, err := s.patchesSince(startCommit, commit)
	if err != nil {
		return err
	}

	// Create snapshot writer for inactive log
	timestamp := time.Now().UTC().Format(time.RFC3339)
	snapWriter, err := s.dLog.NewSnapshotWriter(commit, timestamp)
	if err != nil {
		return fmt.Errorf("failed to create snapshot writer: %w", err)
	}
	snapWriter.SetScopeID(nil)

	// Build snapshot directly to log file (out-of-memory)
	snapIndex := &snap.Index{}
	builder, err := snap.NewBuilder(snapWriter, snapIndex, patchNodes)
	if err != nil {
		snapWriter.Abandon() // Unlock without writing Entry
		return fmt.Errorf("failed to create snapshot builder: %w", err)
	}

	// Apply patches - events flow directly from baseReader → builder → log file
	applier := patches.NewStreamingProcessor()
	if err := applier.ApplyPatches(baseReader, patchNodes, builder); err != nil {
		snapWriter.Abandon()
		return fmt.Errorf("failed to apply patches: %w", err)
	}

	// Close builder to finalize snapshot format (writes index and header)
	// Note: builder.Close() will call snapWriter.Close(), which writes the Entry
	if err := builder.Close(); err != nil {
		// builder.Close() already closed snapWriter, but we should still return the error
		return fmt.Errorf("failed to close snapshot builder: %w", err)
	}

	// builder.Close() called snapWriter.Close(), so Entry is already written

	// Get generation for the snapshot segment
	generation := s.dLog.GetGeneration(snapWriter.LogFileID())

	s.index.Add(index.NewSnapshotSegment(commit, "", string(snapWriter.LogFileID()), snapWriter.EntryPosition(), generation, nil))

	s.logger.Info("snapshot created", "commit", commit, "logFile", snapWriter.LogFileID(), "position", snapWriter.EntryPosition())

	// A scope's patches are deliberately NOT snapshotted here. Only the patches
	// themselves carry op semantics, and a scope needs them to: !key has to merge by
	// identity at read time exactly as it did at the write. So a whole scoped read
	// replays the scope's history, and what bounds a scoped read is the PATH it asks
	// about (narrowSubtreeAt) rather than anything materialized in its place.
	return nil
}

// snapshotEventReadCloser wraps a PathEventReader and its parent Snapshot,
// ensuring both are closed when the reader is done.
type snapshotEventReadCloser struct {
	snapshot *snap.Snapshot
	reader   *snap.PathEventReader
}

func (s *snapshotEventReadCloser) ReadEvent() (*stream.Event, error) {
	return s.reader.ReadEvent()
}

func (s *snapshotEventReadCloser) Close() error {
	// PathEventReader.Close() is a no-op, but call it for consistency
	s.reader.Close()
	// Close the snapshot (which closes the underlying reader)
	return s.snapshot.Close()
}

type sliceEventReader struct {
	events []stream.Event
	i      int
}

func newSliceEventReader(events []stream.Event) *sliceEventReader {
	return &sliceEventReader{events: events}
}

func (ser *sliceEventReader) ReadEvent() (*stream.Event, error) {
	if ser.i == len(ser.events) {
		return nil, io.EOF
	}
	j := ser.i
	ser.i++
	return &ser.events[j], nil
}

func (ser *sliceEventReader) Close() error {
	return nil
}

// patchesSince answers the baseline entries in [from, to], in commit order, for the
// snapshot builders: what a root read folds, held as the builder wants it.
func (s *Storage) patchesSince(from, to int64) ([]*ir.Node, error) {
	var patchNodes []*ir.Node
	for seg := range s.index.Segments("", &from, &to, nil) {
		if seg.StartCommit == seg.EndCommit {
			continue
		}
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
		if err != nil {
			return nil, fmt.Errorf("failed to read patch entry: %w", err)
		}
		if entry.Patch == nil {
			continue
		}
		patchNodes = append(patchNodes, entry.Patch)
	}
	return patchNodes, nil
}

// findSubtreeBaseReader is the SEEK of a read at kp: the nearest baseline snapshot at or
// above kp with the greatest commit at or below `commit` (index.SnapshotAtOrAbove),
// opened at kp's path within it through the snapshot's own index, so the events of one
// subtree stream without the rest being materialized. It answers the reader, the commit
// the fold starts after, and what the seek found -- the root snapshot, a snapshot of a
// path, or nothing, in which case the base is empty and the fold starts at commit 0.
func (s *Storage) findSubtreeBaseReader(commit int64, kp string) (patches.EventReadCloser, int64, seekKind, error) {
	snapSeg, ok := s.index.SnapshotAtOrAbove(kp, commit)
	if !ok {
		return patches.NewEmptyEventReader(), 0, seekMiss, nil
	}
	kind := seekRoot
	if snapSeg.KindedPath != "" {
		kind = seekPath
	}

	entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(snapSeg.LogFile), snapSeg.LogPosition, snapSeg.LogFileGeneration)
	if err != nil {
		return nil, 0, seekMiss, fmt.Errorf("failed to read snapshot entry: %w", err)
	}
	if entry.SnapPos == nil {
		return nil, 0, seekMiss, fmt.Errorf("snapshot entry missing SnapPos")
	}
	snapReader, err := s.dLog.OpenReaderAt(dlog.LogFileID(snapSeg.LogFile), *entry.SnapPos, snapSeg.LogFileGeneration)
	if err != nil {
		return nil, 0, seekMiss, fmt.Errorf("failed to open snapshot reader: %w", err)
	}
	snapshot, err := snap.Open(snapReader)
	if err != nil {
		snapReader.Close()
		return nil, 0, seekMiss, fmt.Errorf("failed to open snapshot: %w", err)
	}
	within := pathWithin(kp, snapSeg.KindedPath)
	eventReader, err := snapshot.ReadPathEventReader(within)
	if err != nil {
		snapshot.Close()
		return nil, 0, seekMiss, fmt.Errorf("error creating event reader at %q: %w", kp, err)
	}
	return &snapshotEventReadCloser{snapshot: snapshot, reader: eventReader}, snapSeg.StartCommit + 1, kind, nil
}
