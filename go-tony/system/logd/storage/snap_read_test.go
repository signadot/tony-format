package storage

import (
	"fmt"
	"testing"
)

// A read must start from the most recent snapshot and replay only the commits after
// it, whatever path it names. Snapshots are indexed at the document root, and the
// lookup used to descend to the READ's path (IterAtPath never consults ancestors), so
// every read below the root found no snapshot, fell back to an empty base, and replayed
// from commit 0 for the life of the document — straight through every snapshot, with no
// sawtooth. The existing snapshot tests all passed: they assert the snapshot is written
// and the state is right, both of which stayed true while nothing read it.
//
// Asserting on startCommit rather than on latency keeps this deterministic. startCommit
// is the whole fix: it is the lower bound of the patch range replayBaselineAt and
// replayScopedAt replay, and it was 0 for every non-root read (bvm163tyh12krwcqcsn0).
func TestSnapshotBoundsReplay(t *testing.T) {
	s := openTestStorage(t)

	for i := 1; i <= 20; i++ {
		mustCommit(t, s, nil, fmt.Sprintf(`{demo: {x: {hot: %d}}}`, i))
	}
	// A path written exactly once, and never again: it too replayed the whole log,
	// because cost was a function of the log, not of the path.
	snapCommit := mustCommit(t, s, nil, `{other: {y: {untouched: "v"}}}`)

	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}

	const after = 3
	var commit int64
	for i := 21; i <= 20+after; i++ {
		commit = mustCommit(t, s, nil, fmt.Sprintf(`{demo: {x: {hot: %d}}}`, i))
	}

	base, startCommit, err := s.findSnapshotBaseReader(commit)
	if err != nil {
		t.Fatalf("findSnapshotBaseReader: %v", err)
	}
	base.Close()
	if want := snapCommit + 1; startCommit != want {
		t.Errorf("startCommit = %d, want %d (the snapshot at commit %d was not found, so the read replays from commit 0)",
			startCommit, want, snapCommit)
	}

	segs := s.index.LookupRange("", &startCommit, &commit, nil)
	if len(segs) != after {
		t.Errorf("replaying %d entries after the snapshot, want %d", len(segs), after)
	}

	// Every path sees the same state: a read is a rooted superset (callers narrow it),
	// so the snapshot base and the patch range must not vary with the path. They used
	// to: the patch range was taken at the read's path, which returned the root's
	// entries PLUS a repeat of each one per level of the path, and the applier paid for
	// each repeat. The results agreed only because merging a whole document twice is a
	// no-op — not a property worth relying on.
	want := mustReadScope(t, s, commit, nil)
	for _, kp := range []string{"demo", "demo.x", "demo.x.hot", "other.y.untouched", "never.written"} {
		got, err := s.ReadStateAt(kp, commit, nil)
		if err != nil {
			t.Fatalf("ReadStateAt(%q): %v", kp, err)
		}
		if !got.DeepEqual(want) {
			t.Errorf("ReadStateAt(%q) differs from the root read", kp)
		}
	}

	// And the state itself survives the snapshot boundary: the latest value from after
	// it, and a value written before it that now lives only inside it.
	hot := nodeAt(want, "demo", "x", "hot")
	if hot == nil || hot.Int64 == nil || *hot.Int64 != int64(20+after) {
		t.Errorf("demo.x.hot after the snapshot = %v, want %d", hot, 20+after)
	}
	if got := getString(want, "other", "y", "untouched"); got != "v" {
		t.Errorf("other.y.untouched = %q, want %q (written before the snapshot, so it comes from the snapshot base)", got, "v")
	}
}

// nodeAt navigates n through the named object fields, returning nil if any is absent.
