package storage

import (
	"fmt"
	"math/rand"
	"testing"
)

// The acceptance test for the overlay spike: the replay path is the definition, so for
// generated interleavings of baseline and scope writes the overlay read must answer
// exactly what a replay answers, at every commit, across more than one overlay.
//
// Non-keyed data only, matching the spike. Keyed arrays need the annotation pre-pass
// (plan R2/P1) and are covered separately.

type wgen struct {
	r *rand.Rand
}

var overlayPaths = []string{"a.x", "a.y", "b", "c.d", "c.e", "top"}

// write emits one random write: which layer, which path, and whether it sets or deletes.
func (g *wgen) write(scope *string) (path, body string, scoped bool) {
	scoped = g.r.Intn(2) == 0
	path = overlayPaths[g.r.Intn(len(overlayPaths))]
	switch g.r.Intn(6) {
	case 0:
		return path, `!delete`, scoped
	case 1:
		return path, fmt.Sprintf(`{nested: %d}`, g.r.Intn(5)), scoped
	case 2:
		return path, `"str"`, scoped
	default:
		return path, fmt.Sprintf(`%d`, g.r.Intn(9)), scoped
	}
}

// readBoth reads the scope at commit through both paths and returns them encoded.
func readBoth(t *testing.T, s *Storage, commit int64, scope string) (overlay, replay string) {
	t.Helper()
	s.EnableScopeOverlay(true)
	ov, err := s.ReadStateAt("", commit, &scope)
	if err != nil {
		t.Fatalf("overlay read at %d: %v", commit, err)
	}
	s.EnableScopeOverlay(false)
	rp, err := s.ReadStateAt("", commit, &scope)
	if err != nil {
		t.Fatalf("replay read at %d: %v", commit, err)
	}
	if ov == nil && rp == nil {
		return "<empty>", "<empty>"
	}
	if ov == nil || rp == nil {
		return fmt.Sprintf("nil=%v", ov == nil), fmt.Sprintf("nil=%v", rp == nil)
	}
	return encodeWire(t, ov), encodeWire(t, rp)
}

// TestScopeOverlay_DifferentialAgainstReplay is the acceptance criterion.
func TestScopeOverlay_DifferentialAgainstReplay(t *testing.T) {
	const cases = 60
	const writesPerCase = 14
	scope := "s1"

	for c := range cases {
		seed := int64(c) + 1
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			s := openTestStorage(t)
			g := &wgen{r: rand.New(rand.NewSource(seed))}

			// Seed a little baseline so paths exist to be shadowed and deleted.
			mustCommit(t, s, nil, `{a: {x: 1, y: 2}, b: 3, c: {d: 4, e: 5}, top: 6}`)

			overlaysWritten := 0
			for i := range writesPerCase {
				path, body, scoped := g.write(&scope)
				var sc *string
				if scoped {
					sc = &scope
				}
				commitAt(t, s, sc, path, body)

				commit, err := s.GetCurrentCommit()
				if err != nil {
					t.Fatalf("GetCurrentCommit: %v", err)
				}

				// Cut an overlay part-way through, and again later, so the test covers a
				// read below the first overlay, between two, and above the last.
				if i == writesPerCase/3 || i == 2*writesPerCase/3 {
					if err := s.WriteScopeOverlay(scope, commit); err != nil {
						t.Fatalf("WriteScopeOverlay at %d: %v", commit, err)
					}
					overlaysWritten++
				}

				got, want := readBoth(t, s, commit, scope)
				if got != want {
					t.Fatalf("commit %d (write %d: %s %q := %s) overlay and replay disagree\n overlay: %s\n replay:  %s",
						commit, i, layerName(scoped), path, body, got, want)
				}
			}

			// Every historical commit too, not just the latest: a read below an overlay
			// must not pick it up, and a read above must.
			last, err := s.GetCurrentCommit()
			if err != nil {
				t.Fatalf("GetCurrentCommit: %v", err)
			}
			for commit := int64(1); commit <= last; commit++ {
				got, want := readBoth(t, s, commit, scope)
				if got != want {
					t.Fatalf("historical read at commit %d disagrees\n overlay: %s\n replay:  %s",
						commit, got, want)
				}
			}
			if overlaysWritten == 0 {
				t.Fatal("no overlay was written; the test proved nothing")
			}
		})
	}
}

func layerName(scoped bool) string {
	if scoped {
		return "scope"
	}
	return "baseline"
}
