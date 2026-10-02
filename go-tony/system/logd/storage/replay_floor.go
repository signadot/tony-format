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

// ErrReplayCompacted is returned by Storage.Deltas when the requested range starts
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
// window returns the surviving subset and Deltas reports success: "no patches between 40
// and 95" is indistinguishable from "nothing changed between 40 and 95", so a client
// whose host was suspended for a day silently loses every transition in between rather
// than being told to re-initialize.
//
// State at a commit below the floor is still readable, and the commit number is still
// valid and never reused (reconcileWatermark). Only the deltas are gone, and a read that
// would need them is answered at a commit the store still holds exactly, and says so
// (AnsweredCommit).

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

// AnsweredCommit is the commit a read asked at `at` is answered at: `at` itself, or a
// later commit when compaction has taken patches the read would fold.
//
// A read seeks a snapshot at or below its commit and folds the patches after it. With the
// newest ROOT snapshot at or below `at` at S, that fold is exact when S is `at` --
// nothing to fold -- or S is at or above the baseline floor, since every baseline patch
// above that floor is kept. Otherwise patches in (S, at] may be gone, and the fold of the
// survivors is a state no commit held, which no commit number could honestly label.
//
// Such a read is answered at the CEILING: the oldest root snapshot at or after `at`. That
// snapshot is what compaction left standing for the commits between S and it, `at` among
// them, so it holds what `at` wrote -- where S holds nothing written since S, `at`'s own
// write included. It also holds what was written after `at`, up to the snapshot: that is
// the snapshot granularity compaction promises beyond its cutoff (compaction.go), and the
// caller is told by the commit. A compaction writes a root snapshot at the switch before
// it drops anything, so there is one at or after every patch it took; with none, which a
// store compacted without that snapshot can have, the answer is the head.
//
// It is a property of the COMMIT, not of a path. A snapshot of a path could make one
// read exact where its ancestors' are not -- and a read at a path reads them too, to say
// why nothing is there, or through a write above it -- so a commit that answered
// differently by path would be two commits under one number. The root's snapshots decide
// for every path, and a read at a root snapshot's commit is exact at all of them.
//
// The read applies this itself (Read, Children, kindFromIndex), so no caller can be
// handed the fold; a caller that has to SAY which commit it read asks here first, and
// reading at the answer is reading at it: an answered commit answers itself.
//
// It does not answer for a scope's term, which folds from commit 0 rather than from a
// snapshot (projectScope; em3dnqpkh12ks0jzqxn0).
func (s *Storage) AnsweredCommit(at int64) int64 {
	floor := s.baselineFloor.Load()
	if floor == 0 {
		return at
	}
	below, _ := s.index.SnapshotCommitAtOrBelow(at)
	if below == at || below >= floor {
		return at
	}
	if after, ok := s.index.SnapshotCommitAtOrAfter(at); ok {
		return after
	}
	if head := s.tick.current(); head > at {
		return head
	}
	return at
}

// baselineFloorFile holds the baseline floor, beside the replay floor.
const baselineFloorFile = "baseline-floor"

// The baseline floor is the highest commit of a BASELINE patch compaction has removed,
// and it is what a read goes by (AnsweredCommit). The replay floor will not do: it rises
// for a scope's dropped entry too -- a deleted scope's go whatever their age -- which
// takes nothing a baseline read folds, so a read that went by it was answered
// approximately with every one of its patches still on disk.

// loadBaselineFloor reads the persisted baseline floor, and says whether one was.
func loadBaselineFloor(root string) (int64, bool, error) {
	data, err := os.ReadFile(filepath.Join(root, "meta", baselineFloorFile))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	if len(data) < 8 {
		return 0, false, fmt.Errorf("invalid baseline floor file: expected 8 bytes, got %d", len(data))
	}
	return int64(binary.LittleEndian.Uint64(data)), true, nil
}

// raiseBaselineFloor persists a new baseline floor and then adopts it, if it is higher
// than the current one: before the destructive step, as raiseReplayFloor is and for its
// reason. Too high costs an approximate answer to a read that had an exact one; too low
// is the mislabelled fold. A floor that is not higher is still written when none is on
// disk yet, so that its being there says it was kept (Open).
func (s *Storage) raiseBaselineFloor(floor int64) error {
	current := s.baselineFloor.Load()
	if floor <= current {
		if s.baselineFloorKept.Load() {
			return nil
		}
		floor = current
	}
	if err := writeFloor(filepath.Join(s.sequence.Root, "meta", baselineFloorFile), floor); err != nil {
		return err
	}
	s.baselineFloorKept.Store(true)
	if floor > current {
		s.baselineFloor.Store(floor)
		s.logger.Info("baseline floor raised; a read is exact from a root snapshot at or above it", "floor", floor)
	}
	return nil
}

// writeFloor writes a floor to its file, whole or not at all.
func writeFloor(path string, floor int64) error {
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
	return nil
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

	if err := writeFloor(filepath.Join(s.sequence.Root, "meta", replayFloorFile), floor); err != nil {
		return err
	}

	s.replayFloor.Store(floor)
	s.logger.Info("replay floor raised; delta replay is exact only above it", "floor", floor)
	return nil
}

// droppedPatchFloor returns the highest commit among the patches that are about to be
// dropped, or 0 if none are.
//
// Only PATCHES count: snapshots are not deltas, so dropping one costs a replay nothing. A
// scope's dropped entry counts as baseline's does. It is dominated -- a later entry of the
// scope restates it -- so a replay that starts after it lands where it would have; but a
// replay that starts before it would be handed the scope's history with a hole in it, and
// the floor is what says where a replay is exact. The floor is one number for the store,
// so a baseline watcher resuming from before a dropped scope entry is told to
// re-initialize too: pessimistic, which is the side this file errs on.
//
// A segment appears once per path its entry touches; taking a maximum is indifferent to
// the repeats.
func droppedPatchFloor(all, survivors []index.LogSegment) int64 {
	return droppedFloor(all, survivors, false)
}

// droppedBaselineFloor is droppedPatchFloor over baseline's patches alone: what the
// baseline floor rises to.
func droppedBaselineFloor(all, survivors []index.LogSegment) int64 {
	return droppedFloor(all, survivors, true)
}

func droppedFloor(all, survivors []index.LogSegment, baselineOnly bool) int64 {
	kept := make(map[int64]map[int64]bool, len(survivors)) // position -> commit -> kept
	for _, seg := range survivors {
		if kept[seg.LogPosition] == nil {
			kept[seg.LogPosition] = make(map[int64]bool)
		}
		kept[seg.LogPosition][seg.EndCommit] = true
	}

	var floor int64
	for _, seg := range all {
		if seg.StartCommit == seg.EndCommit {
			continue // a snapshot
		}
		if baselineOnly && seg.ScopeID != nil {
			continue
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
