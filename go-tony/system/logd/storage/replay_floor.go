package storage

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

// replayFloorFile holds the replay floor, under the store's meta directory.
const replayFloorFile = "replay-floor"

// ErrReplayCompacted is returned by ReadPatchesInRange when the requested range starts
// at or below the replay floor, so the deltas it asks for are no longer all on disk.
// The caller cannot be given every change in the range and must re-initialize from a
// state read instead of resuming.
var ErrReplayCompacted = errors.New("replay range starts below the replay floor; delta history there has been compacted away")

// The replay floor is the highest commit whose individual patch compaction has removed.
// A delta replay is exact only for a range starting ABOVE it.
//
// It exists because the two ends of a commit range fail differently. The published
// watermark (see tick) is the leading edge: never name a commit that is not yet
// readable. The floor is the trailing edge: a commit that WAS published, was read by a
// client, and whose delta has since been deleted. Nothing about having announced commit
// 40 honestly obliges the store to keep 40's patch forever — compaction drops baseline
// patches older than its cutoff (selectSurvivors), which is the documented bargain:
// within the cutoff replay is exact to the commit, beyond it history degrades to
// snapshot granularity.
//
// What the floor adds is that crossing that line is LOUD. Without it, a replay below the
// window returns the surviving subset and ReadPatchesInRange reports success: "no
// patches between 40 and 95" is indistinguishable from "nothing changed between 40 and
// 95", so a client whose host was suspended for a day silently loses every transition in
// between rather than being told to re-initialize.
//
// State at a commit below the floor is still readable, and the commit number is still
// valid and never reused (reconcileWatermark). Only the deltas are gone.

// loadReplayFloor reads the persisted replay floor, or 0 if none has been written.
func loadReplayFloor(root string) (int64, error) {
	data, err := os.ReadFile(filepath.Join(root, "meta", replayFloorFile))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // nothing has been compacted away
		}
		return 0, err
	}
	if len(data) < 8 {
		return 0, fmt.Errorf("invalid replay floor file: expected 8 bytes, got %d", len(data))
	}
	return int64(binary.LittleEndian.Uint64(data)), nil
}

// ReplayFloor returns the highest commit whose delta history has been compacted away.
// An exact delta replay is available for ranges starting above it. Zero means no
// history has been dropped.
func (s *Storage) ReplayFloor() int64 {
	return s.replayFloor.Load()
}

// raiseReplayFloor persists a new floor and then adopts it, if it is higher than the
// current one.
//
// Persist BEFORE the destructive step that justifies it, never after. The two failure
// directions are not symmetric: a floor higher than reality costs a spurious
// ErrReplayCompacted, and the client re-initializes — correct, merely pessimistic. A
// floor lower than reality is silent data loss, which is the thing this exists to
// prevent. Crashing between the write and the compaction therefore has to leave the
// floor too high, not too low.
//
// It is also why the floor does not live in index.gob: compaction deletes log records,
// not just index entries, so a discarded-and-rebuilt index (persistedIndexStale) would
// take the floor with it while the log stayed compacted — back to silent loss.
func (s *Storage) raiseReplayFloor(floor int64) error {
	if floor <= s.replayFloor.Load() {
		return nil
	}

	path := filepath.Join(s.sequence.Root, "meta", replayFloorFile)
	tmp := path + ".tmp"

	var data [8]byte
	binary.LittleEndian.PutUint64(data[:], uint64(floor))
	if err := os.WriteFile(tmp, data[:], 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}

	s.replayFloor.Store(floor)
	s.logger.Info("replay floor raised; delta replay is exact only above it", "floor", floor)
	return nil
}

// droppedPatchFloor returns the highest baseline commit among segments that are about to
// be dropped, or 0 if none are.
//
// Only BASELINE PATCHES count. Snapshots are not deltas, so dropping one costs a replay
// nothing. Scope patches are never dropped by cutoff (selectSurvivors retains a scope's
// whole overlay until DeleteScope), and a scope's removal is not a statement about
// baseline history, so neither belongs in a store-wide floor.
//
// A segment appears once per path its entry touches; taking a maximum is indifferent to
// the repeats.
func droppedPatchFloor(all, survivors []index.LogSegment) int64 {
	kept := make(map[int64]map[int64]bool, len(survivors)) // position -> commit -> kept
	for _, seg := range survivors {
		if kept[seg.LogPosition] == nil {
			kept[seg.LogPosition] = make(map[int64]bool)
		}
		kept[seg.LogPosition][seg.EndCommit] = true
	}

	var floor int64
	for _, seg := range all {
		if seg.StartCommit == seg.EndCommit || seg.ScopeID != nil {
			continue // snapshot, or a scope's retained overlay
		}
		if kept[seg.LogPosition][seg.EndCommit] {
			continue
		}
		if seg.EndCommit > floor {
			floor = seg.EndCommit
		}
	}
	return floor
}
