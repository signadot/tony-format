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
// Caller is responsible for closing the returned reader.
func (s *Storage) findSnapshotBaseReader(kp string, commit int64) (patches.EventReadCloser, int64, error) {
	iter := s.index.IterAtPath(kp)
	if !iter.Valid() {
		return nil, 0, fmt.Errorf("invalid index iterator for path %q", kp)
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

		// Open reader at snapshot position to read the header
		snapReader, err := s.dLog.OpenReaderAt(dlog.LogFileID(seg.LogFile), *entry.SnapPos)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to open snapshot reader: %w", err)
		}

		// Open the snapshot to parse header and get event stream
		snapshot, err := snap.Open(snapReader)
		if err != nil {
			snapReader.Close()
			return nil, 0, fmt.Errorf("failed to open snapshot: %w", err)
		}
		defer snapshot.Close()
		node, err := snapshot.ReadPath(kp)
		if err != nil {
			return nil, 0, fmt.Errorf("error reading %q from snapshot: %w", kp, err)
		}
		events, err := stream.NodeToEvents(node)
		if err != nil {
			return nil, 0, fmt.Errorf("errro translating node to events: %w", err)
		}
		return newSliceEventReader(events), seg.StartCommit + 1, nil
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
	if err := s.createSnapshot(commit); err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	return nil
}

// createSnapshot creates a snapshot of the full state at the given commit.
// Writes snapshot events to the inactive log and adds an index entry.
// Returns error if snapshot creation fails.
func (s *Storage) createSnapshot(commit int64) error {
	// Find most recent snapshot and get base event reader
	baseReader, startCommit, err := s.findSnapshotBaseReader("", commit)
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

	// Create snapshot writer for inactive log
	timestamp := time.Now().UTC().Format(time.RFC3339)
	snapWriter, err := s.dLog.NewSnapshotWriter(commit, timestamp)
	if err != nil {
		return fmt.Errorf("failed to create snapshot writer: %w", err)
	}

	// Build snapshot directly to log file (out-of-memory)
	snapIndex := &snap.Index{}
	builder, err := snap.NewBuilder(snapWriter, snapIndex, patchNodes)
	if err != nil {
		snapWriter.Abandon() // Unlock without writing Entry
		return fmt.Errorf("failed to create snapshot builder: %w", err)
	}

	// Apply patches - events flow directly from baseReader → builder → log file
	applier := patches.NewInMemoryApplier()
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

	snapSeg := &index.LogSegment{
		StartCommit: commit,
		EndCommit:   commit,
		StartTx:     0,
		EndTx:       0,
		KindedPath:  "",
		LogFile:     string(snapWriter.LogFileID()),
		LogPosition: snapWriter.EntryPosition(),
	}
	s.index.Add(snapSeg)

	s.logger.Info("snapshot created", "commit", commit, "logFile", snapWriter.LogFileID(), "position", snapWriter.EntryPosition())
	return nil
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
