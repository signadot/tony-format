package storage

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// A precondition decides whether a write lands, so a scoped one built on the baseline head
// must answer exactly what a full replay answers. This checks that over generated
// histories, at the commit a precondition is actually evaluated at -- the current one.
func TestScopedHead_AgreesWithReplay(t *testing.T) {
	const cases = 40
	scope := "s1"
	for c := range cases {
		seed := int64(c) + 1
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			s := openTestStorage(t)
			g := &wgen{r: rand.New(rand.NewSource(seed))}
			mustCommit(t, s, nil, `{a: {x: 1, y: 2}, b: 3, c: {d: 4, e: 5}, top: 6}`)

			for i := range 12 {
				path, body, scoped := g.write(&scope)
				var sc *string
				if scoped {
					sc = &scope
				}
				commitAt(t, s, sc, path, body)
				if i == 3 || i == 8 {
					if err := s.SwitchDLog(); err != nil {
						t.Fatalf("SwitchDLog: %v", err)
					}
				}

				commit, err := s.GetCurrentCommit()
				if err != nil {
					t.Fatalf("GetCurrentCommit: %v", err)
				}

				// Seed the baseline head the way a preceding precondition would, then ask
				// both ways under the lock a real precondition holds.
				s.commitMu.Lock()
				if _, err := s.steppedBaselineAt(commit); err != nil {
					s.commitMu.Unlock()
					t.Fatalf("seed head: %v", err)
				}
				viaHead, errH := s.steppedScopedAt(commit, &scope)
				s.commitMu.Unlock()
				if errH != nil {
					t.Fatalf("steppedScopedAt: %v", errH)
				}
				viaReplay, err := s.replayScopedAt(commit, &scope)
				if err != nil {
					t.Fatalf("replay: %v", err)
				}
				if got, want := nodeOrEmpty(t, viaHead), nodeOrEmpty(t, viaReplay); got != want {
					t.Fatalf("commit %d (write %d: %s %q := %s) precondition disagrees with replay\n head:   %s\n replay: %s",
						commit, i, layerName(scoped), path, body, got, want)
				}
			}
		})
	}
}

func nodeOrEmpty(t *testing.T, n *ir.Node) string {
	t.Helper()
	if n == nil {
		return "<empty>"
	}
	return encodeWire(t, n)
}

// wgen emits a random interleaving of baseline and scope writes over overlapping paths.
// It was the scope overlay differential's generator; scope_head_test is what still uses
// it now that the overlay is gone.
type wgen struct {
	r *rand.Rand
}

var scopeGenPaths = []string{"a.x", "a.y", "b", "c.d", "c.e", "top"}

// write emits one random write: which layer, which path, and whether it sets or deletes.
func (g *wgen) write(scope *string) (path, body string, scoped bool) {
	scoped = g.r.Intn(2) == 0
	path = scopeGenPaths[g.r.Intn(len(scopeGenPaths))]
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

func layerName(scoped bool) string {
	if scoped {
		return "scope"
	}
	return "baseline"
}
