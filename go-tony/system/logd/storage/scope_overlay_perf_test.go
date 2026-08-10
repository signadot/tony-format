package storage

import (
	"fmt"
	"testing"
)

// Does the overlay do what section 1 of the plan wants: turn a scoped read from
// O(scope writes) into something bounded by what the scope has written since the
// overlay was cut?
func TestScopeOverlay_ReadCost(t *testing.T) {
	if testing.Short() {
		t.Skip("scaling measurement")
	}
	const reps = 20
	scope := "s1"

	t.Log("scoped read, N scope writes, overlay cut at N (so nothing replays after it):")
	for _, n := range []int{50, 100, 200, 400} {
		s := openTestStorage(t)
		mustCommit(t, s, nil, `{seed: 0}`)
		for i := 0; i < n; i++ {
			commitAt(t, s, &scope, "a.x", fmt.Sprintf("%d", i))
		}
		commit, err := s.GetCurrentCommit()
		if err != nil {
			t.Fatalf("GetCurrentCommit: %v", err)
		}

		s.EnableScopeOverlay(false)
		replay := timeN(reps, func() {
			if _, err := s.ReadStateAt("", commit, &scope); err != nil {
				t.Fatalf("read: %v", err)
			}
		})

		if err := s.WriteScopeOverlay(scope, commit); err != nil {
			t.Fatalf("WriteScopeOverlay: %v", err)
		}
		s.EnableScopeOverlay(true)
		overlay := timeN(reps, func() {
			if _, err := s.ReadStateAt("", commit, &scope); err != nil {
				t.Fatalf("read: %v", err)
			}
		})

		t.Logf("  N=%4d  replay %-12v overlay %v", n, replay, overlay)
	}
}
