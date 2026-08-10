package storage

import (
	"fmt"
	"testing"
)

// The plan's section 1 names three costs a scope pays that baseline does not. The spike
// addressed the read; the other two -- a conditional write's precondition and a watcher's
// per-event recompute -- both route through ReadStateAt, so they may already be fixed by
// the same change. This measures all three with the overlay off and on, so what is left
// for stepping (plan steps 7-8) is a number rather than an assumption.
func TestScopeOverlay_AllThreeCosts(t *testing.T) {
	if testing.Short() {
		t.Skip("scaling measurement")
	}
	const reps = 20
	scope := "s1"

	// setup builds a store with n scope writes and an overlay cut at the end, the state a
	// snapshot leaves behind.
	setup := func(t *testing.T, n int) (*Storage, int64) {
		s := openTestStorage(t)
		mustCommit(t, s, nil, `{seed: {x: 0}, tag: "t"}`)
		for i := 0; i < n; i++ {
			commitAt(t, s, &scope, "seed.x", fmt.Sprintf("%d", i))
		}
		commit, err := s.GetCurrentCommit()
		if err != nil {
			t.Fatalf("GetCurrentCommit: %v", err)
		}
		if err := s.WriteScopeOverlay(scope, commit); err != nil {
			t.Fatalf("WriteScopeOverlay: %v", err)
		}
		return s, commit
	}

	for _, on := range []bool{false, true} {
		label := "overlay OFF (replay)"
		if on {
			label = "overlay ON"
		}
		t.Logf("%s:", label)
		t.Logf("  %5s  %-12s %-12s %s", "N", "read", "CAS write", "watch/event")
		for _, n := range []int{50, 100, 200, 400} {
			s, commit := setup(t, n)
			s.EnableScopeOverlay(on)

			read := timeN(reps, func() {
				if _, err := s.ReadStateAt("", commit, &scope); err != nil {
					t.Fatalf("read: %v", err)
				}
			})

			// A watcher's per-event work is the same recompute (session.go scopedDocAt).
			watch := timeN(reps, func() {
				if _, err := s.ReadStateAt("", commit, &scope); err != nil {
					t.Fatalf("recompute: %v", err)
				}
			})

			match := matchTag(t)
			cas := timeN(reps, func() {
				scalingCommit(t, s, &scope, `{tag: "t"}`, match)
			})

			t.Logf("  %5d  %-12v %-12v %v", n, read, cas, watch)
		}
	}
}
