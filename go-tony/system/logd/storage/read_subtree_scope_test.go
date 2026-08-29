package storage

import (
	"math/rand"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// A narrow SCOPED read has to answer exactly what the wide scoped read holds at that
// path. That is the whole contract; everything else about it is cost.
//
// The wide read is the oracle rather than a second store, because the wide scoped read
// is the definition of a scope layer -- baseline replayed, then the scope's own patches
// last -- and the narrow read is that same pass asked at a path. There is nothing else
// for it to be right against.
//
// The stream is the mixed one the scope differentials use: baseline and scope writes over
// overlapping paths, so a scope write is regularly followed by a baseline write beneath,
// above or at it, which is where a narrowing that got the shadowing wrong would show.
// Reads are taken at every path including ones nothing ever writes, and at every commit,
// because a narrowing fails on a SHAPE -- an op at an ancestor, mixed patch-root depth --
// rather than on a value.
func TestNarrowScopedReadMatchesTheWideRead(t *testing.T) {
	// Both streams: the plain one, and the one carrying the RELATIVE operations a scope
	// stores as a claim (lower.go). A claim is stored as a different patch than the
	// client sent, so a narrowing which projected it wrongly would only show here.
	for _, gen := range []struct {
		name string
		ops  func(*rand.Rand, int) []scopeOp
	}{{"plain", genScopeOps}, {"relative", genClaimOps}} {
		t.Run(gen.name, func(t *testing.T) { narrowScopedReadMatches(t, gen.ops) })
	}
}

func narrowScopedReadMatches(t *testing.T, gen func(*rand.Rand, int) []scopeOp) {
	const scope = "s1"
	sc := scope
	checked, narrowed := 0, 0

	for seed := 1; seed <= seedCount(); seed++ {
		rng := rand.New(rand.NewSource(int64(seed)))
		ops := gen(rng, 30)

		s := openTestStorage(t)

		for i, o := range ops {
			c, err := applyScopeOp(t, s, o, scope)
			if err != nil {
				continue // did not apply to the document as it stood
			}
			if o.snapshot {
				if err := s.SwitchDLog(); err != nil {
					t.Fatalf("SwitchDLog: %v", err)
				}
			}

			wide, err := s.ReadStateAt("", c, &sc)
			if err != nil {
				t.Fatalf("seed %d op %d: wide scoped read: %v", seed, i, err)
			}
			for _, kp := range readPaths {
				if kp == "" {
					continue // the root is not a narrowing
				}
				if wide == nil {
					// An empty document holds nothing anywhere, and navigating one is
					// not a question ir answers.
					continue
				}
				want, err := wide.GetKPathWith(kp, ir.WithComments(true))
				if err != nil {
					// The wide read cannot be navigated there -- an ancestor is a
					// scalar, say. That is the wide read's answer to give, and the
					// narrow read declines those; nothing to compare.
					continue
				}
				got, ok, err := s.ReadSubtreeAt(kp, c, &sc)
				if err != nil {
					t.Fatalf("seed %d op %d: narrow scoped read %q: %v", seed, i, kp, err)
				}
				checked++
				if !ok {
					// Declined, so the caller reads wide and gets the oracle. That the
					// caller does read wide when declined is checked at the caller.
					continue
				}
				narrowed++
				switch {
				case want == nil && got == nil:
				case want == nil || got == nil:
					t.Fatalf("seed %d op %d (%s): %q: narrow gave %v, wide gave %v",
						seed, i, o, kp, got, want)
				case !got.DeepEqual(want):
					t.Fatalf("seed %d op %d (%s): %q: narrow differs from the wide read\n"+
						" got %s\nwant %s", seed, i, o, kp,
						mustEncode(t, got), mustEncode(t, want))
				}
			}
		}
	}
	t.Logf("NARROW SCOPE seeds=%d reads=%d narrowed=%d", seedCount(), checked, narrowed)
}

// The point of it: reading one path out of a scope does not replay the scope's history.
// A scope which has written N times to one place and once to another answers the second
// without touching the first.
func TestNarrowScopedReadDoesNotReplayTheScope(t *testing.T) {
	if testing.Short() {
		t.Skip("scaling measurement")
	}
	const reps = 20
	scope := "s1"

	t.Log("scoped read at a path the scope wrote once, with N scope writes elsewhere:")
	for _, n := range []int{50, 100, 200, 400} {
		s := openTestStorage(t)
		mustCommit(t, s, nil, `{seed: 0}`)
		commitAt(t, s, &scope, "b.y", "1")
		for i := 0; i < n; i++ {
			commitAt(t, s, &scope, "a.x", string(rune('0'+i%10)))
		}
		commit, err := s.GetCurrentCommit()
		if err != nil {
			t.Fatalf("GetCurrentCommit: %v", err)
		}

		wide := timeN(reps, func() {
			if _, err := s.ReadStateAt("b.y", commit, &scope); err != nil {
				t.Fatalf("wide: %v", err)
			}
		})
		var got *ir.Node
		narrow := timeN(reps, func() {
			n, ok, err := s.ReadSubtreeAt("b.y", commit, &scope)
			if err != nil || !ok {
				t.Fatalf("narrow declined or failed: ok=%v err=%v", ok, err)
			}
			got = n
		})
		if got == nil {
			t.Fatalf("N=%d: the scope's own write at b.y read back as nothing", n)
		}
		t.Logf("  N=%4d  wide %-12v narrow %v", n, wide, narrow)
	}
}
