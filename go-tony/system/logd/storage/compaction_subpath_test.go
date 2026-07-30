package storage

import (
	"fmt"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// Every log entry is indexed at the document root AND at every path inside its patch
// (indexPatchRec). Compaction sources its work list from the root and navigates by
// KindedPath, so it only ever maintained the root copies: the below-root copies of a
// dropped or moved entry were left behind pointing at a position that no longer holds
// them. Anything reaching an entry through a below-root segment — watch replay does —
// then read a stale position.
//
// Reads every segment reachable from a written path after compaction. A survivor must
// read back; nothing may point at a position compaction moved or freed.
func TestCompactionMaintainsBelowRootSegments(t *testing.T) {
	s := openTestStorage(t)

	for i := 1; i <= 5; i++ {
		mustCommit(t, s, nil, fmt.Sprintf(`{demo: {x: {hot: %d}}}`, i))
	}
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	for i := 6; i <= 8; i++ {
		mustCommit(t, s, nil, fmt.Sprintf(`{demo: {x: {hot: %d}}}`, i))
	}
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}

	before := s.index.LookupRangeAll("demo.x.hot", nil, nil)
	t.Logf("segments reachable from demo.x.hot before compaction: %d", len(before))

	cfg := &CompactionConfig{
		Cutoff:       0, // everything is past the cutoff
		BaseInterval: time.Hour,
		SlotsPerTier: 8,
		Multiplier:   2,
		GracePeriod:  100 * time.Millisecond,
	}
	if err := s.Compact(cfg); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	after := s.index.LookupRangeAll("demo.x.hot", nil, nil)
	t.Logf("segments reachable from demo.x.hot after compaction: %d", len(after))

	var unreadable int
	for _, seg := range after {
		if _, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration); err != nil {
			unreadable++
			if unreadable <= 5 {
				t.Errorf("stale below-root segment kp=%q commit=%d %s@%d gen=%d: %v",
					seg.KindedPath, seg.StartCommit, seg.LogFile, seg.LogPosition,
					seg.LogFileGeneration, err)
			}
		}
	}
	if unreadable > 0 {
		t.Errorf("%d of %d segments reachable from demo.x.hot are unreadable after compaction",
			unreadable, len(after))
	}
}

// The second mechanism, and the one a Cutoff-0 test alone will miss: an entry that is
// RETAINED but MOVED by the file rewrite. With Cutoff 0 every baseline patch is dropped and
// only snapshots survive, and snapshots are indexed at the root only — so they have no
// below-root copies and the repositioning path never runs. Scope patches are retained
// regardless of cutoff ("Retain every scope patch until the scope is deleted"), so one
// scope patch sharing a log file with baseline patches the cutoff drops constructs it
// directly: the entry survives, the rewrite moves it, its root segment is repositioned, and
// its below-root copies used to be left at the old position and generation forever.
//
// The data is readable at the root the whole time; it is reachability through the path
// index that breaks, which is what a path-scoped consumer resolves.
func TestCompactionRepositionsBelowRootCopiesOfRetainedEntry(t *testing.T) {
	s := openTestStorage(t)
	scope := "s1"

	for i := 1; i <= 4; i++ {
		mustCommit(t, s, nil, fmt.Sprintf(`{demo: {x: {hot: %d}}}`, i))
	}
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}

	// Everything from here goes to the log that the second switch makes inactive, which is
	// the one compaction rewrites. The baseline patches go in FIRST so the scope patch sits
	// at a non-zero offset and the rewrite actually moves it — if it were first in the file
	// it would stay at position 0 and only its generation would change.
	for i := 5; i <= 7; i++ {
		mustCommit(t, s, nil, fmt.Sprintf(`{demo: {x: {hot: %d}}}`, i))
	}
	// Retained regardless of cutoff, and indexed below the root at demo, demo.x and
	// demo.x.scoped — so it is the entry that must be repositioned rather than dropped.
	mustCommit(t, s, &scope, `{demo: {x: {scoped: "keep me"}}}`)
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}

	cfg := &CompactionConfig{
		Cutoff:       0, // drops the baseline patches, forcing a rewrite that moves the scope patch
		BaseInterval: time.Hour,
		SlotsPerTier: 8,
		Multiplier:   2,
		GracePeriod:  100 * time.Millisecond,
	}
	if err := s.Compact(cfg); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	// Where the root says the retained scope entry now lives. Identify it by scope rather
	// than by the commit mustCommit returned: a segment's StartCommit is the base it
	// applies to, one less than the commit it produced.
	var rootPos, rootGen int64 = -1, -1
	var rootFile string
	var startCommit, startTx int64
	for _, seg := range s.index.LookupRangeAll("", nil, nil) {
		if seg.ScopeID != nil && *seg.ScopeID == scope {
			rootPos, rootGen, rootFile = seg.LogPosition, seg.LogFileGeneration, seg.LogFile
			startCommit, startTx = seg.StartCommit, seg.StartTx
		}
	}
	if rootPos < 0 {
		t.Fatal("retained scope entry is not in the root index after compaction")
	}
	t.Logf("retained scope entry start=%d/tx=%d root copy at %s@%d gen=%d",
		startCommit, startTx, rootFile, rootPos, rootGen)

	var checked int
	for _, kp := range []string{"demo", "demo.x", "demo.x.scoped"} {
		for _, seg := range s.index.LookupRangeAll(kp, nil, nil) {
			if seg.StartCommit != startCommit || seg.StartTx != startTx ||
				seg.ScopeID == nil || *seg.ScopeID != scope {
				continue
			}
			checked++
			if seg.LogPosition != rootPos || seg.LogFileGeneration != rootGen || seg.LogFile != rootFile {
				t.Errorf("kp=%q: below-root copy at %s@%d gen=%d, root copy at %s@%d gen=%d",
					kp, seg.LogFile, seg.LogPosition, seg.LogFileGeneration, rootFile, rootPos, rootGen)
			}
			if _, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration); err != nil {
				t.Errorf("kp=%q: below-root copy unreadable: %v", kp, err)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no below-root copies of the retained scope entry found — the test is not exercising the reposition path")
	}
	t.Logf("checked %d below-root copies of the retained entry", checked)

	// The consumer-visible symptom this mechanism produced. The range is the retained
	// entry's own commit, not [0, ...]: this compaction dropped the baseline patches
	// below it, so a range starting at 0 is now correctly refused with
	// ErrReplayCompacted, and would say nothing about whether the deep path resolves.
	entryCommit := startCommit + 1
	if _, err := s.ReadPatchesInRange("demo.x.scoped", entryCommit, entryCommit, &scope); err != nil {
		t.Errorf("ReadPatchesInRange at the deep path: %v", err)
	}
}

// The root and below-root copies of one entry must stay in agreement: they are the same
// entry, so they must name the same position and generation. Compaction moved the root
// copy and left the others behind, which is the mechanism behind the stale reads above.
func TestCompactionKeepsRootAndSubpathCopiesInSync(t *testing.T) {
	s := openTestStorage(t)

	for i := 1; i <= 5; i++ {
		mustCommit(t, s, nil, fmt.Sprintf(`{demo: {x: {hot: %d}}}`, i))
	}
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	for i := 6; i <= 8; i++ {
		mustCommit(t, s, nil, fmt.Sprintf(`{demo: {x: {hot: %d}}}`, i))
	}
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}

	cfg := &CompactionConfig{
		Cutoff:       0,
		BaseInterval: time.Hour,
		SlotsPerTier: 8,
		Multiplier:   2,
		GracePeriod:  100 * time.Millisecond,
	}
	if err := s.Compact(cfg); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	type place struct {
		pos, gen int64
		file     string
	}
	// entry identity -> where the root copy says it lives
	root := map[[2]int64]place{}
	for _, seg := range s.index.LookupRangeAll("", nil, nil) {
		root[[2]int64{seg.StartCommit, seg.StartTx}] = place{seg.LogPosition, seg.LogFileGeneration, seg.LogFile}
	}

	for _, kp := range []string{"demo", "demo.x", "demo.x.hot"} {
		for _, seg := range s.index.LookupRangeAll(kp, nil, nil) {
			id := [2]int64{seg.StartCommit, seg.StartTx}
			r, ok := root[id]
			if !ok {
				t.Errorf("kp=%q commit=%d survives below root but was dropped at the root",
					kp, seg.StartCommit)
				continue
			}
			got := place{seg.LogPosition, seg.LogFileGeneration, seg.LogFile}
			if got != r {
				t.Errorf("kp=%q commit=%d: below-root copy at %s@%d gen=%d, root copy at %s@%d gen=%d",
					kp, seg.StartCommit, got.file, got.pos, got.gen, r.file, r.pos, r.gen)
			}
		}
	}
}
