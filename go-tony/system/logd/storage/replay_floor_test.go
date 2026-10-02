package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
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
	if _, err := readPatchesInRange(s, "", 1, 4, nil); err != nil {
		t.Fatalf("ReadPatchesInRange before compaction: %v", err)
	}

	compactAwayEverything(t, s)

	floor := s.ReplayFloor()
	if floor == 0 {
		t.Fatal("floor still 0 after compaction dropped every patch")
	}

	_, err = readPatchesInRange(s, "", 1, floor+10, nil)
	if !errors.Is(err, ErrReplayCompacted) {
		t.Errorf("replay from below the floor returned err = %v, want ErrReplayCompacted", err)
	}

	// Above the floor is still exact, so it must not error.
	if _, err := readPatchesInRange(s, "", floor+1, floor+10, nil); err != nil {
		t.Errorf("replay from above the floor (%d) returned err = %v, want nil", floor+1, err)
	}
}

// The floor bounds only DELTA replay. State at a commit below it is still readable, and
// the commit number is still valid -- but a read there cannot be answered AT it when
// patches it would fold are gone, since the survivors fold to a state no commit held. It
// is answered at the commit the store holds exactly, the snapshot it starts from, and
// AnsweredCommit says which (cpqj2tf6h12kr5jxqxn0).
//
// So for every commit: the answer is at or below it, the state read there is exactly the
// state written through it, and the head is answered at the head.
func TestReplayFloor_StateBelowFloorStillReadable(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// The state after n commits holds k0 .. k(n-1). Snapshots fall at 2 and 4, and the
	// history between them is compacted away.
	want := []string{""}
	acc := ""
	for i := range 6 {
		commitValue(t, s, fmt.Sprintf("{k%d: %d}", i, i))
		acc += fmt.Sprintf("k%d: %d\n", i, i)
		want = append(want, acc)
		if i == 1 || i == 3 {
			if err := s.SwitchDLog(); err != nil {
				t.Fatalf("SwitchDLog: %v", err)
			}
		}
	}
	cfg := DefaultCompactionConfig()
	cfg.Cutoff = -time.Hour
	if err := s.Compact(cfg); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	floor := s.ReplayFloor()
	if floor == 0 {
		t.Fatal("expected a non-zero floor")
	}

	head := int64(len(want) - 1)
	approximated := 0
	for c := int64(0); c <= head; c++ {
		at := s.AnsweredCommit(c, "")
		if at > c {
			t.Errorf("a read at %d is answered at %d, after it", c, at)
			continue
		}
		if at != c {
			approximated++
		}
		got, err := readStateAt(s, "", at, nil)
		if err != nil {
			t.Errorf("read at %d (answering %d): %v", at, c, err)
			continue
		}
		if !sameState(t, got, want[at]) {
			t.Errorf("a read at %d answers %d with %s, which is not the state at %d: %s",
				c, at, show(got), at, want[at])
		}
	}
	if at := s.AnsweredCommit(head, ""); at != head {
		t.Errorf("the head %d is answered at %d", head, at)
	}
	if approximated == 0 {
		t.Errorf("no read was answered earlier than asked, with floor %d: the case is not exercised", floor)
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
	if _, err := readPatchesInRange(s2, "", 1, floor+10, nil); !errors.Is(err, ErrReplayCompacted) {
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

	if err := os.Remove(filepath.Join(dir, "index.manifest")); err != nil {
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

// Only dropped PATCHES raise the floor. A snapshot is not a delta, so losing one costs a
// replay nothing; a scope's dropped entry counts as baseline's does, since a replay of
// the scope from before it would have a hole where it was.
func TestDroppedPatchFloor_CountsDroppedPatchesOfAnyScope(t *testing.T) {
	scope := "s1"
	segs := []index.LogSegment{
		{StartCommit: 20, EndCommit: 20, LogPosition: 10},                  // snapshot at 20
		{StartCommit: 18, EndCommit: 19, LogPosition: 20, ScopeID: &scope}, // scope patch
		{StartCommit: 10, EndCommit: 11, LogPosition: 30},                  // baseline patch
		{StartCommit: 11, EndCommit: 12, LogPosition: 40},                  // baseline patch
	}
	// The snapshot at 20 outranks everything and never counts. With the scope patch
	// kept, the floor is the dropped baseline patch at 11; with it dropped, 19.
	if got := droppedPatchFloor(segs, []index.LogSegment{segs[1], segs[3]}); got != 11 {
		t.Errorf("droppedPatchFloor = %d, want 11 (the dropped baseline patch alone)", got)
	}
	if got := droppedPatchFloor(segs, []index.LogSegment{segs[3]}); got != 19 {
		t.Errorf("droppedPatchFloor = %d, want 19 (the dropped scope patch)", got)
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

// sameState says the state read is the one written: src in tony, "" for nothing.
func sameState(t *testing.T, got *ir.Node, src string) bool {
	t.Helper()
	if src == "" {
		return got == nil
	}
	want, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return got != nil && got.DeepEqual(want)
}

func show(n *ir.Node) string {
	if n == nil {
		return "nothing"
	}
	return encode.MustString(n)
}
