package storage

import (
	"fmt"
	"testing"
)

// What a scoped WATCH pays per event.
//
// A watch is a read. A baseline watcher steps a document it keeps, and a scoped one
// cannot -- its writes apply last and shadow baseline stickily, so folding a baseline
// patch into a materialized scoped document lets baseline overwrite a leaf the scope
// owns. So a scoped watcher re-reads its view per event, and what that costs is what this
// measures.
//
// logd once answered this with a stepper built on a scope overlay, which is gone. The
// answer now is that the re-read is a read AT THE WATCHED PATH, and a scoped read at a
// path narrows: it replays the patches which bear on that path, not the scope's history.
// A watcher on a quiet path in a busy scope should therefore be flat in the scope's
// accumulated writes -- which is what the stepper was for.
func TestScopedWatchReadCostPerEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("scaling measurement")
	}
	const reps = 20
	scope := "s1"

	t.Log("per-event scoped re-read at a watched path, N accumulated scope writes elsewhere:")
	for _, n := range []int{50, 100, 200, 400} {
		s := openTestStorage(t)
		mustCommit(t, s, nil, `{seed: {x: 0}, other: 0}`)
		commitAt(t, s, &scope, "watched.leaf", "1")
		for i := 0; i < n; i++ {
			commitAt(t, s, &scope, "seed.x", fmt.Sprintf("%d", i))
		}
		commit, err := s.GetCurrentCommit()
		if err != nil {
			t.Fatalf("GetCurrentCommit: %v", err)
		}

		// What the watcher's recompute did before a scoped read could narrow.
		wide := timeN(reps, func() {
			if _, err := s.ReadStateAt("watched.leaf", commit, &scope); err != nil {
				t.Fatalf("wide: %v", err)
			}
		})
		// What it does now: readDocAt's narrow read at the watched path.
		var got bool
		narrow := timeN(reps, func() {
			_, ok, err := s.ReadSubtreeRootedAt("watched.leaf", commit, &scope)
			if err != nil {
				t.Fatalf("narrow: %v", err)
			}
			got = ok
		})
		if !got {
			t.Fatalf("N=%d: the watched path declined to narrow, so a watcher still reads wide", n)
		}
		t.Logf("  N=%4d  recompute-wide %-12v recompute-narrow %v", n, wide, narrow)
	}
}
