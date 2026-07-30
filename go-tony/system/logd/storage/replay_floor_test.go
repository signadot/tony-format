package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

// compactAwayEverything runs a compaction whose cutoff is in the future, so every
// baseline patch in the inactive log is older than it and gets dropped.
func compactAwayEverything(t *testing.T, s *Storage) {
	t.Helper()
	if err := s.dLog.SwitchActive(); err != nil {
		t.Fatalf("SwitchActive: %v", err)
	}
	cfg := DefaultCompactionConfig()
	cfg.Cutoff = -time.Hour // cutoffTime is in the future: nothing is "within cutoff"
	if err := s.Compact(cfg); err != nil {
		t.Fatalf("Compact: %v", err)
	}
}

// A replay whose range starts below the floor must be reported, not answered with the
// subset that happens to survive: an empty or short list is indistinguishable from a
// quiet period, so the client would take erased history for "nothing happened".
func TestReplayFloor_TruncatedRangeIsReported(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	for i := range 4 {
		commitValue(t, s, fmt.Sprintf("{k%d: %d}", i, i))
	}
	if got := s.ReplayFloor(); got != 0 {
		t.Fatalf("floor before compaction = %d, want 0", got)
	}

	// A replay across the whole range works while the history is intact.
	if _, err := s.ReadPatchesInRange("", 1, 4, nil); err != nil {
		t.Fatalf("ReadPatchesInRange before compaction: %v", err)
	}

	compactAwayEverything(t, s)

	floor := s.ReplayFloor()
	if floor == 0 {
		t.Fatal("floor still 0 after compaction dropped every patch")
	}

	_, err = s.ReadPatchesInRange("", 1, floor+10, nil)
	if !errors.Is(err, ErrReplayCompacted) {
		t.Errorf("replay from below the floor returned err = %v, want ErrReplayCompacted", err)
	}

	// Above the floor is still exact, so it must not error.
	if _, err := s.ReadPatchesInRange("", floor+1, floor+10, nil); err != nil {
		t.Errorf("replay from above the floor (%d) returned err = %v, want nil", floor+1, err)
	}
}

// The floor bounds only DELTA replay. State at a commit below it is still readable, and
// the commit number is still valid — that distinction is the whole point of reporting
// truncation rather than refusing the commit.
func TestReplayFloor_StateBelowFloorStillReadable(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	for i := range 4 {
		commitValue(t, s, fmt.Sprintf("{k%d: %d}", i, i))
	}
	compactAwayEverything(t, s)

	floor := s.ReplayFloor()
	if floor == 0 {
		t.Fatal("expected a non-zero floor")
	}
	if _, err := s.ReadStateAt("", floor, nil); err != nil {
		t.Errorf("ReadStateAt(%d) below the floor returned err = %v, want nil", floor, err)
	}
}

// The floor must survive a restart. Compaction deletes log records, so a floor that came
// back as 0 would put the store back to answering doomed replays with short lists.
func TestReplayFloor_SurvivesReopen(t *testing.T) {
	dir := t.TempDir()

	s1, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for i := range 4 {
		commitValue(t, s1, fmt.Sprintf("{k%d: %d}", i, i))
	}
	compactAwayEverything(t, s1)
	floor := s1.ReplayFloor()
	if floor == 0 {
		t.Fatal("expected a non-zero floor")
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	if got := s2.ReplayFloor(); got != floor {
		t.Errorf("floor after reopen = %d, want %d", got, floor)
	}
	if _, err := s2.ReadPatchesInRange("", 1, floor+10, nil); !errors.Is(err, ErrReplayCompacted) {
		t.Errorf("after reopen, replay from below the floor returned err = %v, want ErrReplayCompacted", err)
	}
}

// Specifically: it must not live only in index.gob, because a discarded index is rebuilt
// from a log that is still compacted.
func TestReplayFloor_SurvivesIndexLoss(t *testing.T) {
	dir := t.TempDir()

	s1, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for i := range 4 {
		commitValue(t, s1, fmt.Sprintf("{k%d: %d}", i, i))
	}
	compactAwayEverything(t, s1)
	floor := s1.ReplayFloor()
	if floor == 0 {
		t.Fatal("expected a non-zero floor")
	}
	// Commit after compacting so the index has content to persist — and to lose.
	for i := range 2 {
		commitValue(t, s1, fmt.Sprintf("{post%d: %d}", i, i))
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := os.Remove(filepath.Join(dir, "index.gob")); err != nil {
		t.Fatalf("remove index: %v", err)
	}

	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	if got := s2.ReplayFloor(); got != floor {
		t.Errorf("floor after losing the index = %d, want %d", got, floor)
	}
}

// The floor never moves backwards: a later compaction that drops nothing must not lower
// what an earlier one recorded.
func TestReplayFloor_NeverLowered(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	for i := range 4 {
		commitValue(t, s, fmt.Sprintf("{k%d: %d}", i, i))
	}
	compactAwayEverything(t, s)
	floor := s.ReplayFloor()
	if floor == 0 {
		t.Fatal("expected a non-zero floor")
	}

	if err := s.raiseReplayFloor(floor - 1); err != nil {
		t.Fatalf("raiseReplayFloor: %v", err)
	}
	if got := s.ReplayFloor(); got != floor {
		t.Errorf("floor = %d after trying to lower it, want %d", got, floor)
	}
}

// Only dropped BASELINE PATCHES may raise the floor. A snapshot is not a delta, so
// losing one costs a replay nothing, and a scope keeps its whole overlay until the scope
// itself is deleted — neither is a statement about baseline history.
func TestDroppedPatchFloor_CountsOnlyBaselinePatches(t *testing.T) {
	scope := "s1"
	segs := []index.LogSegment{
		{StartCommit: 20, EndCommit: 20, LogPosition: 10},                  // snapshot at 20
		{StartCommit: 18, EndCommit: 19, LogPosition: 20, ScopeID: &scope}, // scope patch
		{StartCommit: 10, EndCommit: 11, LogPosition: 30},                  // baseline patch
		{StartCommit: 11, EndCommit: 12, LogPosition: 40},                  // baseline patch
	}
	// Everything is dropped except the last baseline patch. The snapshot at 20 and the
	// scope patch at 19 both outrank the dropped baseline patch at 11, so a floor that
	// counted them would come back as 20 rather than 11.
	survivors := []index.LogSegment{segs[3]}

	if got := droppedPatchFloor(segs, survivors); got != 11 {
		t.Errorf("droppedPatchFloor = %d, want 11 (the dropped baseline patch alone)", got)
	}
}

func TestDroppedPatchFloor_NothingDropped(t *testing.T) {
	segs := []index.LogSegment{
		{StartCommit: 1, EndCommit: 2, LogPosition: 10},
		{StartCommit: 2, EndCommit: 3, LogPosition: 20},
	}
	if got := droppedPatchFloor(segs, segs); got != 0 {
		t.Errorf("droppedPatchFloor with everything surviving = %d, want 0", got)
	}
}

// The same entry is indexed once per path it touches, so the dropped set contains
// repeats; a survivor at one path must not look like a survivor at another.
func TestDroppedPatchFloor_RepeatedSegmentsPerPath(t *testing.T) {
	segs := []index.LogSegment{
		{StartCommit: 4, EndCommit: 5, LogPosition: 10, KindedPath: ""},
		{StartCommit: 4, EndCommit: 5, LogPosition: 10, KindedPath: "a"},
		{StartCommit: 4, EndCommit: 5, LogPosition: 10, KindedPath: "a.b"},
	}
	// All three name one entry at position 10; keeping any of them keeps the entry.
	if got := droppedPatchFloor(segs, []index.LogSegment{segs[1]}); got != 0 {
		t.Errorf("droppedPatchFloor = %d, want 0 (the entry survives at every path)", got)
	}
	if got := droppedPatchFloor(segs, nil); got != 5 {
		t.Errorf("droppedPatchFloor with the entry dropped = %d, want 5", got)
	}
}
