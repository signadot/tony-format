package storage

import (
	"fmt"
	"math/rand"
	"testing"
)

// A watcher's stepped view must equal what a read says, at every commit -- that is the
// property that lets stepping replace recomputing rather than an assumption that it can.
// Generated interleavings, with snapshots landing mid-stream so a new overlay is cut under
// the stepper.
func TestScopedWatchStepper_AgreesWithRead(t *testing.T) {
	const cases = 40
	scope := "s1"
	for c := range cases {
		seed := int64(c) + 1
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			s := openTestStorage(t)
			g := &wgen{r: rand.New(rand.NewSource(seed))}
			mustCommit(t, s, nil, `{a: {x: 1, y: 2}, b: 3, c: {d: 4, e: 5}, top: 6}`)

			// Everything the watcher will be handed, in commit order.
			notes := make(chan *CommitNotification, 64)
			s.SetCommitNotifier(func(n *CommitNotification) { notes <- n })

			start, err := s.GetCurrentCommit()
			if err != nil {
				t.Fatalf("GetCurrentCommit: %v", err)
			}
			w, err := s.NewScopedWatchStepper(scope, start)
			if err != nil {
				t.Fatalf("NewScopedWatchStepper: %v", err)
			}
			if w == nil {
				t.Skip("scope not serviceable by a stepper")
			}

			for i := range 14 {
				path, body, scoped := g.write(&scope)
				var sc *string
				if scoped {
					sc = &scope
				}
				commitAt(t, s, sc, path, body)
				if i == 4 || i == 9 {
					if err := s.SwitchDLog(); err != nil {
						t.Fatalf("SwitchDLog: %v", err)
					}
				}

				n := <-notes
				got, err := w.Step(n)
				if err != nil {
					t.Fatalf("Step: %v", err)
				}
				want, err := s.ReadStateAt("", n.Commit, &scope)
				if err != nil {
					t.Fatalf("read: %v", err)
				}
				if a, b := nodeOrEmpty(t, got), nodeOrEmpty(t, want); a != b {
					t.Fatalf("commit %d (write %d: %s %q := %s) stepped view disagrees with the read\n stepped: %s\n read:    %s",
						n.Commit, i, layerName(scoped), path, body, a, b)
				}
			}
		})
	}
}

// And the per-event cost, against the recompute it replaces.
func TestScopedWatchStepper_CostPerEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("scaling measurement")
	}
	const reps = 20
	scope := "s1"
	t.Log("per delivered event, N accumulated scope writes:")
	for _, n := range []int{50, 100, 200, 400} {
		s := openTestStorage(t)
		s.EnableScopeOverlay(true) // the stepper is the overlay's; it is not the default
		mustCommit(t, s, nil, `{seed: {x: 0}, other: 0}`)
		for i := 0; i < n; i++ {
			commitAt(t, s, &scope, "seed.x", fmt.Sprintf("%d", i))
		}
		commit, _ := s.GetCurrentCommit()
		if err := s.WriteScopeOverlay(scope, commit); err != nil {
			t.Fatalf("WriteScopeOverlay: %v", err)
		}

		recompute := timeN(reps, func() {
			if _, err := s.ReadStateAt("", commit, &scope); err != nil {
				t.Fatalf("recompute: %v", err)
			}
		})

		w, err := s.NewScopedWatchStepper(scope, commit)
		if err != nil || w == nil {
			t.Fatalf("NewScopedWatchStepper: %v", err)
		}
		note := &CommitNotification{Commit: commit, Patch: mustParseBody(t, `{other: 1}`)}
		stepped := timeN(reps, func() {
			if _, err := w.Step(note); err != nil {
				t.Fatalf("Step: %v", err)
			}
		})
		t.Logf("  N=%4d  recompute %-12v stepped %v", n, recompute, stepped)
	}
}
