package storage

import (
	"bytes"
	"fmt"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/patches"
)

// findSnapshotBaseReader searches for the most recent snapshot <= commit
// and returns an EventReadCloser starting from that snapshot, plus the startCommit
// for patches that should be applied after it.
// Caller is responsible for closing the returned reader.
func (s *Storage) findSnapshotBaseReader(commit int64) (patches.EventReadCloser, int64, error) {
	iter := s.index.IterAtPath("")
	if !iter.Valid() {
		return nil, 0, fmt.Errorf("invalid index iterator for root path")
	}

	s.index.RLock()
	defer s.index.RUnlock()

	for seg := range iter.CommitsAt(commit, index.Down) {
		// Skip non-snapshot entries (patches have StartCommit != EndCommit)
		if seg.StartCommit != seg.EndCommit {
			continue
		}

		// Found snapshot - load events from it
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to read snapshot entry: %w", err)
		}
		if entry.SnapPos == nil {
			return nil, 0, fmt.Errorf("snapshot entry missing SnapPos")
		}

		// Open reader at snapshot position
		snapReader, err := s.dLog.OpenReaderAt(dlog.LogFileID(seg.LogFile), *entry.SnapPos)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to open snapshot reader: %w", err)
		}

		baseReader := patches.NewSnapshotEventReader(snapReader)
		startCommit := seg.StartCommit + 1
		s.logger.Debug("found snapshot", "commit", seg.StartCommit, "position", seg.LogPosition)
		return baseReader, startCommit, nil
	}

	// No snapshot found - start from empty (null state at commit 0)
	return patches.NewEmptyEventReader(), 0, nil
}

// SwitchAndSnapshot switches the active log and creates a snapshot of the inactive log.
// The snapshot is created for the current commit at the time of switching.
// This should be called periodically (e.g., based on log size or time) to enable
// snapshot-based read optimization and eventual compaction.
// Protected by switchMu to prevent concurrent switching while snapshot is being written.
func (s *Storage) SwitchAndSnapshot() error {
	s.switchMu.Lock()
	defer s.switchMu.Unlock()

	// Get current commit before switching
	commit, err := s.GetCurrentCommit()
	if err != nil {
		return fmt.Errorf("failed to get current commit: %w", err)
	}

	// Switch active log
	if err := s.dLog.SwitchActive(); err != nil {
		return fmt.Errorf("failed to switch active log: %w", err)
	}

	// Create snapshot of the inactive log (which was active before switch)
	// This is a long operation, but switchMu prevents switching back during it
	if err := s.CreateSnapshot(commit); err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	return nil
}

// CreateSnapshot creates a snapshot of the full state at the given commit.
// Writes snapshot events to the inactive log and adds an index entry.
// Returns error if snapshot creation fails.
func (s *Storage) CreateSnapshot(commit int64) error {
	// Find most recent snapshot and get base event reader
	baseReader, startCommit, err := s.findSnapshotBaseReader(commit)
	if err != nil {
		return err
	}
	defer baseReader.Close()

	// Get patches from startCommit to commit
	segments := s.index.LookupRange("", &startCommit, &commit)

	// Extract patch nodes, filtering out snapshots
	var patchNodes []*ir.Node
	for _, seg := range segments {
		// Skip snapshots (StartCommit == EndCommit)
		if seg.StartCommit == seg.EndCommit {
			continue
		}

		// Read patch from dlog
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition)
		if err != nil {
			return fmt.Errorf("failed to read patch entry: %w", err)
		}
		if entry.Patch == nil {
			continue
		}

		patchNodes = append(patchNodes, entry.Patch)
	}

	// Apply patches using PatchApplier interface
	eventBuffer := &bytes.Buffer{}
	sink := stream.NewBufferEventSink(eventBuffer)
	applier := patches.NewInMemoryApplier()

	if err := applier.ApplyPatches(baseReader, patchNodes, sink); err != nil {
		return fmt.Errorf("failed to apply patches: %w", err)
	}

	// Write snapshot to inactive log
	timestamp := time.Now().UTC().Format(time.RFC3339)
	entryPos, logFile, err := s.dLog.WriteSnapshotToInactive(commit, timestamp, eventBuffer.Bytes())
	if err != nil {
		return fmt.Errorf("failed to write snapshot to log: %w", err)
	}

	// Add index entry for snapshot
	snapSeg := &index.LogSegment{
		StartCommit: commit,
		EndCommit:   commit,
		StartTx:     0,
		EndTx:       0,
		KindedPath:  "",
		LogFile:     string(logFile),
		LogPosition: entryPos,
	}
	s.index.Add(snapSeg)

	s.logger.Info("snapshot created", "commit", commit, "logFile", logFile, "position", entryPos)
	return nil
}
