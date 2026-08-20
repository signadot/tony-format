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

// findSnapshotBaseReader searches for the most recent baseline snapshot <= commit
// and returns an EventReadCloser over the whole document at that snapshot, plus the
// startCommit for patches that should be applied after it. Scope snapshots are not
// created, so only baseline snapshots are considered; scoped reads layer the scope's
// patches over this baseline (see replayScopedAt).
//
// The lookup is at the document ROOT, not at the read's path, and the base it returns
// is the whole document, not a subtree. Both follow from what is on either side of it:
// createSnapshot indexes the snapshot segment with KindedPath "" (root), and the
// patches layered on top of this base are whole-document entries, so a subtree base
// would not align with them. This is why the read's kpath is not a parameter — reads
// are rooted supersets that callers trim (see replayBaselineAt, session.go
// scopedDocAt). If reads ever become genuinely path-scoped (patches sub-extracted at
// the path), a subtree base via snapshot.ReadPathEventReader(kp) becomes right again.
//
// Looking the snapshot up at the read's path instead — IterAtPath(kp) descends to kp's
// index node and never consults ancestors — found nothing for every non-root read, so
// every such read fell back to an empty base and replayed from commit 0 for the life of
// the document, straight through each snapshot (issue bvm163tyh12krwcqcsn0).
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

// baselineSnapshotSegment answers the most recent baseline snapshot at or below
// commit. The lookup is at the ROOT because that is where a snapshot is indexed;
// see findSnapshotBaseReader for what looking it up at a read's path did.
func (s *Storage) baselineSnapshotSegment(commit int64) (index.LogSegment, bool) {
	iter := s.index.IterAtPath("")

	// Find the segment while holding the lock, then release before any I/O:
	// LogFile and LogPosition are immutable once written.
	s.index.RLock()
	defer s.index.RUnlock()
	for seg := range iter.CommitsAt(commit, index.Down) {
		// Skip non-snapshot entries (patches have StartCommit != EndCommit)
		if seg.StartCommit != seg.EndCommit {
			continue
		}
		// Only baseline snapshots.
		if seg.ScopeID == nil {
			return seg, true
		}
	}
	return index.LogSegment{}, false
}

// SwitchDLog switches the active log and creates snapshots.
// Creates a baseline snapshot plus snapshots for all active scopes.
// The snapshots are created for the current commit at the time of switching.
// This should be called periodically (e.g., based on log size or time) to enable
// snapshot-based read optimization and eventual compaction.
//
// Concurrency: dlog handles coordination internally via per-file snapMu locks.
// SwitchActive blocks if a snapshot is in progress on the inactive log.
// createSnapshot returns ErrSnapshotInProgress if called while another snapshot is running.
func (s *Storage) SwitchDLog() error {
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
	// materialized scope overlay resolves !key away and is unsound to re-apply onto a
	// changed baseline. The scope layer is read as raw op-preserving patches instead
	// (see replayScopedAt). Bounded scope-overlay compaction: 5hmq80f3h12krh1mbsn0.
	if err := s.createSnapshot(commit); err != nil {
		return fmt.Errorf("failed to create baseline snapshot: %w", err)
	}

	// The stepped head is a second way of computing the same state, so check it against
	// a full read here — the one place a full read is already the order of the work
	// being done. Any drift is then bounded by the snapshot interval instead of running
	// until something downstream notices. See head.go.
	s.CheckHead()

	// Say what reads have been doing since the last snapshot. A store cannot be
	// asked afterwards, and from outside a narrow read and a wide one differ only in
	// how long they took -- which is exactly what is in doubt when a fix does not
	// show up downstream (ap8ddvp2h12krd43gdn0).
	if rs := s.ReadStats(); rs.Narrow+rs.WideRoot+rs.WideScope+rs.WideOperator+rs.WideAbsent > 0 {
		s.logger.Info("reads since start",
			"narrow", rs.Narrow, "wideRoot", rs.WideRoot, "wideScope", rs.WideScope,
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
// Scope snapshots are no longer created (a materialized scope overlay is unsound for
// !key); the scope layer is read as raw op-preserving patches instead. See
// replayScopedAt and issue 5hmq80f3h12krh1mbsn0.
func (s *Storage) createSnapshot(commit int64) error {
	// Find most recent snapshot and get base event reader
	baseReader, startCommit, err := s.findSnapshotBaseReader(commit)
	if err != nil {
		return err
	}
	defer baseReader.Close()

	// Get patches from startCommit to commit
	segments := s.index.LookupRange("", &startCommit, &commit, nil)

	// Extract patch nodes, filtering out snapshots
	var patchNodes []*ir.Node
	for _, seg := range segments {
		// Skip snapshots (StartCommit == EndCommit)
		if seg.StartCommit == seg.EndCommit {
			continue
		}

		// Read patch from dlog
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
		if err != nil {
			return fmt.Errorf("failed to read patch entry: %w", err)
		}
		if entry.Patch == nil {
			continue
		}

		patchNodes = append(patchNodes, entry.Patch)
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

	snapSeg := &index.LogSegment{
		StartCommit:       commit,
		EndCommit:         commit,
		StartTx:           0,
		EndTx:             0,
		KindedPath:        "",
		LogFile:           string(snapWriter.LogFileID()),
		LogPosition:       snapWriter.EntryPosition(),
		LogFileGeneration: generation,
		ScopeID:           nil,
	}
	s.index.Add(snapSeg)

	s.logger.Info("snapshot created", "commit", commit, "logFile", snapWriter.LogFileID(), "position", snapWriter.EntryPosition())

	// SPIKE (docs/scope_overlay_plan.md): give each live scope the same treatment the
	// baseline just had. Scope patches are exempt from snapshotting, which is why a
	// scoped read replays the scope's whole history; an overlay written here bounds it
	// to what the scope has written since. Gated with the read path, so a store with the
	// flag off is byte-identical to before.
	if s.scopeOverlay {
		s.writeScopeOverlays(commit)
	}
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
