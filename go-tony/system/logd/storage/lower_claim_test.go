package storage

import (
	"testing"
)

// A scope's write is a standing CLAIM, not a difference.
//
// A scope's patches replay over a baseline that moves, so what the scope stores has
// to keep meaning what it meant when it was written, whatever baseline does next. A
// difference cannot: it is the effect against one baseline, and where the effect is
// smaller than the claim, the claim is gone.
//
// The case that shows it is a delete of a path baseline has not created yet. The
// write changes only the spine, so a diff says `a: {}`, which merges to nothing --
// and the scope stops shadowing that path forever after, so baseline's later write
// shows straight through into a scope that had said no to it.
//
// See claimDelta. Each row is: what the scope says, then what baseline does after,
// then what the scope must still read.
func TestAScopeStoresClaimsNotDifferences(t *testing.T) {
	tests := []struct {
		name             string
		seed             string
		scopePath, scope string
		basePath, base   string
		want             string
	}{{
		name:      "a delete of what baseline has not written yet",
		seed:      `{z: 0}`,
		scopePath: "a.b", scope: `!delete`,
		basePath: "a.b", base: `{k: 1}`,
		want: "a: {} z: 0",
	}, {
		// The same shape one level up: the scope deletes a whole subtree that is
		// not there, and baseline fills it in.
		name:      "a delete of a subtree baseline then creates",
		seed:      `{z: 0}`,
		scopePath: "a", scope: `!delete`,
		basePath: "a", base: `{b: {k: 1}}`,
		want: "z: 0",
	}, {
		// A relative op that DOES change something still has to hold its ground:
		// the scope appends, baseline replaces the array underneath.
		name:      "an append baseline overwrites",
		seed:      `{xs: [1]}`,
		scopePath: "xs", scope: `!arraydiff {1: !insert 2}`,
		basePath: "xs", base: `[9]`,
		want: "xs: [ 1 2 ]",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := openTestStorage(t)
			s.LowerEverything(true)
			mustCommit(t, s, nil, test.seed)

			const scope = "s1"
			if _, err := applyScopeOp(t, s, scopeOp{
				scoped: true, path: test.scopePath, src: test.scope,
			}, scope); err != nil {
				t.Fatalf("the scope's write: %v", err)
			}
			if _, err := applyScopeOp(t, s, scopeOp{
				path: test.basePath, src: test.base,
			}, scope); err != nil {
				t.Fatalf("baseline's write: %v", err)
			}

			commit, err := s.GetCurrentCommit()
			if err != nil {
				t.Fatalf("GetCurrentCommit: %v", err)
			}
			sc := scope
			doc, err := s.ReadStateAt("", commit, &sc)
			if err != nil {
				t.Fatalf("scoped read: %v", err)
			}
			if got := flatten(t, doc); got != test.want {
				t.Errorf("the scope reads\n  %s\nwant\n  %s", got, test.want)
			}
		})
	}
}
