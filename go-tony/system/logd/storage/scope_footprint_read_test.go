package storage

import (
	"fmt"
	"math/rand"
	"testing"
)

// THE FOOTPRINT CHANGES A COST, NEVER AN ANSWER. Two stores take the same mixed stream of
// baseline and scoped writes; one reads its scopes from the footprint, the other folds
// every entry of the scope from the index, as every scoped read once did. Every read, in
// both views, at every path, at every commit, agrees byte for byte -- the reads at past
// commits included, which is where the footprint declines and the history answers.
func TestScopeFootprintDifferential(t *testing.T) {
	const scope = "s1"
	paths := []string{"", "a", "a.b", "a.b.c", "d", "d.e", "k0", "k1"}
	var historic int64
	for seed := 1; seed <= seedCount(); seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(int64(seed)))
			ops := genScopeOps(rng, 60)
			ref := openTestStorage(t)
			ref.scopeReadsHistoric = true
			subj := openTestStorage(t)
			var commits []int64
			for i, o := range ops {
				rc, rerr := applyScopeOp(t, ref, o, scope)
				sc, serr := applyScopeOp(t, subj, o, scope)
				if (rerr == nil) != (serr == nil) || rc != sc {
					t.Fatalf("op %d %s: reference %d %v, subject %d %v", i, o, rc, rerr, sc, serr)
				}
				if rerr == nil {
					commits = append(commits, rc)
				}
				if o.snapshot {
					for _, s := range []*Storage{ref, subj} {
						if err := s.SwitchDLog(); err != nil {
							t.Fatalf("op %d: SwitchDLog: %v", i, err)
						}
					}
				}
			}
			compareViews(t, ref, subj, commits, paths, scope, ops)
			st := subj.ReadStats()
			if st.ScopeSkipped != 0 {
				t.Errorf("the footprint handed reads %d statements they did not fold", st.ScopeSkipped)
			}
			if st.ScopeFootprint == 0 {
				t.Errorf("no read was served from the footprint")
			}
			historic += st.ScopeHistoric
		})
	}
	if historic == 0 {
		t.Errorf("no read at a past commit was served from the history; the rule was not exercised")
	}
}

// A scoped read at a path the index proves unwritten, under a scope that wrote elsewhere,
// opens no log file: the footprint says the scope does not reach it either.
func TestScopedReadAtANeverWrittenPathOpensNoLog(t *testing.T) {
	s := openTestStorage(t)
	sc := "s1"
	mustCommit(t, s, nil, `{a: {x: 1}}`)
	commitAt(t, s, &sc, "d.e", `5`)
	head, _ := s.GetCurrentCommit()
	before := s.ReadStats()
	got, _, err := readSubtreeAt(s, "a.nowhere", head, &sc)
	if err != nil || got != nil {
		t.Fatalf("read a.nowhere in the scope: %v, %v", got, err)
	}
	after := s.ReadStats()
	if after.NarrowAbsent != before.NarrowAbsent+1 || after.Scope != before.Scope {
		t.Errorf("the read was not answered from the index: absent %d -> %d, scoped reads %d -> %d",
			before.NarrowAbsent, after.NarrowAbsent, before.Scope, after.Scope)
	}
	// And a path the scope reaches from above is not proven absent by baseline's index.
	commitAt(t, s, &sc, "a", `!insert.raw {y: {z: 2}}`)
	head, _ = s.GetCurrentCommit()
	got, _, err = readSubtreeAt(s, "a.y.z", head, &sc)
	if err != nil || got == nil || got.Int64 == nil || *got.Int64 != 2 {
		t.Errorf("a.y.z under the scope's claim reads %v, %v; want 2", got, err)
	}
}
