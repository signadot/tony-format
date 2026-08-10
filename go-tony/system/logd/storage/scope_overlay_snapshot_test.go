package storage

import (
	"fmt"
	"math/rand"
	"testing"
)

func countOverlays(s *Storage, scopeID string) int {
	n := 0
	for _, seg := range s.index.AllSegments() {
		if isOverlaySegment(seg) && *seg.ScopeID == scopeID && seg.KindedPath == "" {
			n++
		}
	}
	return n
}

// TestScopeOverlay_WrittenBySnapshot: SwitchDLog now refreshes each live scope's overlay
// alongside baseline's snapshot, and reads stay equal to a replay across it.
func TestScopeOverlay_WrittenBySnapshot(t *testing.T) {
	s := openTestStorage(t)
	s.EnableScopeOverlay(true)
	a, b := "sa", "sb"

	mustCommit(t, s, nil, `{seed: {x: 0}}`)
	for i := 0; i < 5; i++ {
		commitAt(t, s, &a, "seed.x", fmt.Sprintf("%d", i))
		commitAt(t, s, &b, "other", fmt.Sprintf("%d", i))
	}
	if got := countOverlays(s, a); got != 0 {
		t.Fatalf("no snapshot yet, but %d overlays for %q", got, a)
	}

	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	for _, sc := range []string{a, b} {
		if got := countOverlays(s, sc); got != 1 {
			t.Errorf("after one snapshot, scope %q has %d overlays, want 1", sc, got)
		}
	}

	commit, _ := s.GetCurrentCommit()
	for _, sc := range []string{a, b} {
		got, want := readBoth(t, s, commit, sc)
		if got != want {
			t.Errorf("scope %q after snapshot: overlay %s, replay %s", sc, got, want)
		}
	}
	s.EnableScopeOverlay(true)

	// A second snapshot with nothing written in between must not add another overlay.
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog 2: %v", err)
	}
	for _, sc := range []string{a, b} {
		if got := countOverlays(s, sc); got != 1 {
			t.Errorf("after an idle snapshot, scope %q has %d overlays, want still 1", sc, got)
		}
	}

	// Write to one scope only; the next snapshot refreshes that one alone.
	commitAt(t, s, &a, "seed.x", "99")
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog 3: %v", err)
	}
	if got := countOverlays(s, a); got != 2 {
		t.Errorf("scope %q wrote since the last overlay: %d overlays, want 2", a, got)
	}
	if got := countOverlays(s, b); got != 1 {
		t.Errorf("scope %q was idle: %d overlays, want still 1", b, got)
	}
}

// TestScopeOverlay_DifferentialAcrossSnapshots is the differential again, but with the
// overlays cut by SwitchDLog rather than by the test, and baseline snapshots moving under
// them.
func TestScopeOverlay_DifferentialAcrossSnapshots(t *testing.T) {
	const cases = 25
	scope := "s1"
	for c := range cases {
		seed := int64(c) + 1
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			s := openTestStorage(t)
			s.EnableScopeOverlay(true)
			g := &wgen{r: rand.New(rand.NewSource(seed))}
			mustCommit(t, s, nil, `{a: {x: 1, y: 2}, b: 3, c: {d: 4, e: 5}, top: 6}`)

			for i := range 12 {
				path, body, scoped := g.write(&scope)
				var sc *string
				if scoped {
					sc = &scope
				}
				commitAt(t, s, sc, path, body)
				if i == 3 || i == 7 {
					if err := s.SwitchDLog(); err != nil {
						t.Fatalf("SwitchDLog: %v", err)
					}
					s.EnableScopeOverlay(true)
				}
				commit, err := s.GetCurrentCommit()
				if err != nil {
					t.Fatalf("GetCurrentCommit: %v", err)
				}
				if got, want := readBoth(t, s, commit, scope); got != want {
					t.Fatalf("commit %d: overlay %s, replay %s", commit, got, want)
				}
				s.EnableScopeOverlay(true)
			}
			last, _ := s.GetCurrentCommit()
			for commit := int64(1); commit <= last; commit++ {
				if got, want := readBoth(t, s, commit, scope); got != want {
					t.Fatalf("historical commit %d: overlay %s, replay %s", commit, got, want)
				}
				s.EnableScopeOverlay(true)
			}
		})
	}
}
