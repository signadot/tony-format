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

// findSnapshotBaseReader searches for the most recent snapshot <= commit
// for a given path and returns an EventReadCloser starting from that snapshot, plus the startCommit
// for patches that should be applied after it.
//
// For scoped reads (scopeID != nil):
//   - First looks for a scope-specific snapshot
//   - Falls back to baseline snapshot if no scope snapshot exists
//
// Caller is responsible for closing the returned reader.
func (s *Storage) findSnapshotBaseReader(kp string, commit int64, scopeID *string) (patches.EventReadCloser, int64, error) {
	iter := s.index.IterAtPath(kp)
	if !iter.Valid() {
		return nil, 0, fmt.Errorf("invalid index iterator for path %q", kp)
	}

	// Find snapshot segment while holding lock, then release before I/O.
	// Segment data (LogFile, LogPosition) is immutable once written,
	// so it's safe to use after releasing the lock.
	var snapSeg *index.LogSegment
	var baselineSnapSeg *index.LogSegment // fallback for scoped reads
	s.index.RLock()
	for seg := range iter.CommitsAt(commit, index.Down) {
		// Skip non-snapshot entries (patches have StartCommit != EndCommit)
		if seg.StartCommit != seg.EndCommit {
			continue
		}

		// For scoped reads, first try to find a scope-specific snapshot
		if scopeID != nil {
			if seg.ScopeID != nil && *seg.ScopeID == *scopeID {
				// Found scope-specific snapshot
				segCopy := seg
				snapSeg = &segCopy
				break
			}
			// Remember baseline snapshot as fallback
			if seg.ScopeID == nil && baselineSnapSeg == nil {
				segCopy := seg
				baselineSnapSeg = &segCopy
			}
			continue
		}

		// Baseline read: only consider baseline snapshots
		if seg.ScopeID == nil {
			segCopy := seg
			snapSeg = &segCopy
			break
		}
	}
	s.index.RUnlock()

	// For scoped reads, use baseline snapshot as fallback
	if snapSeg == nil && scopeID != nil && baselineSnapSeg != nil {
		snapSeg = baselineSnapSeg
	}

	// No snapshot found - start from empty (null state at commit 0)
	if snapSeg == nil {
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

	// Get streaming event reader for the path (no in-memory materialization)
	eventReader, err := snapshot.ReadPathEventReader(kp)
	if err != nil {
		snapshot.Close()
		return nil, 0, fmt.Errorf("error creating event reader for %q from snapshot: %w", kp, err)
	}

	// Return wrapper that closes both the event reader and snapshot when done
	return &snapshotEventReadCloser{snapshot: snapshot, reader: eventReader}, snapSeg.StartCommit + 1, nil
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

	// Get active scopes before switching (and clear the set)
	activeScopes := s.getAndClearActiveScopes()

	// Switch active log - blocks if snapshot in progress on inactive log
	if err := s.dLog.SwitchActive(); err != nil {
		return fmt.Errorf("failed to switch active log: %w", err)
	}

	// Create scope snapshots first, before baseline.
	// This ensures scope snapshots can use the previous baseline snapshot as base
	// and correctly include all scope patches up to the current commit.
	for _, scopeID := range activeScopes {
		if err := s.createSnapshot(commit, &scopeID); err != nil {
			// Log error but continue with other scopes
			s.logger.Error("failed to create scope snapshot", "scopeID", scopeID, "error", err)
		}
	}

	// Create baseline snapshot last
	if err := s.createSnapshot(commit, nil); err != nil {
		return fmt.Errorf("failed to create baseline snapshot: %w", err)
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

// CreateScopeSnapshot creates a snapshot of scope state at a specific commit.
// The snapshot captures the combined baseline + scope state, enabling efficient
// reads for frequently accessed scopes without replaying all patches.
// This is useful for long-running scopes (e.g., experiments) with many patches.
func (s *Storage) CreateScopeSnapshot(scopeID string, commit int64) error {
	return s.createSnapshot(commit, &scopeID)
}

// createSnapshot creates a snapshot of the full state at the given commit.
// Writes snapshot events to the inactive log and adds an index entry.
// For baseline snapshots, pass nil for scopeID.
// For scope snapshots, pass the scope ID - the snapshot will include baseline + scope patches.
// Returns error if snapshot creation fails.
func (s *Storage) createSnapshot(commit int64, scopeID *string) error {
	// Find most recent snapshot and get base event reader
	baseReader, startCommit, err := s.findSnapshotBaseReader("", commit, scopeID)
	if err != nil {
		return err
	}
	defer baseReader.Close()

	// Get patches from startCommit to commit
	segments := s.index.LookupRange("", &startCommit, &commit, scopeID)

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
	snapWriter.SetScopeID(scopeID)

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
		ScopeID:           scopeID,
	}
	s.index.Add(snapSeg)

	if scopeID != nil {
		s.logger.Info("scope snapshot created", "commit", commit, "scopeID", *scopeID, "logFile", snapWriter.LogFileID(), "position", snapWriter.EntryPosition())
	} else {
		s.logger.Info("snapshot created", "commit", commit, "logFile", snapWriter.LogFileID(), "position", snapWriter.EntryPosition())
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
