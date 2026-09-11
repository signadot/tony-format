package tony

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
)

// entitySet is the document the fold's cost was measured on (v552mdbqh12kr7dtgdn0,
// rkb7p8v5h12ksdnmgsn0): a set of n entities under verse.entities.
func entitySet(n int) *ir.Node {
	set := make(map[string]*ir.Node, n)
	for i := 0; i < n; i++ {
		id := "e" + strconv.Itoa(i)
		set[id] = ir.FromMap(map[string]*ir.Node{"id": ir.FromString(id), "status": ir.FromString("new")})
	}
	return ir.FromMap(map[string]*ir.Node{
		"verse": ir.FromMap(map[string]*ir.Node{"entities": ir.FromMap(set)}),
	})
}

// entityWrite writes one field of entity i.
func entityWrite(i int, status string) *ir.Node {
	return ir.FromMap(map[string]*ir.Node{
		"verse": ir.FromMap(map[string]*ir.Node{
			"entities": ir.FromMap(map[string]*ir.Node{
				"e" + strconv.Itoa(i): ir.FromMap(map[string]*ir.Node{"status": ir.FromString(status)}),
			}),
		}),
	})
}

// storeOpts are the options the store folds with (api.NextState).
var storeOpts = []mergeop.PatchOpt{mergeop.Comments(true), mergeop.RejectUnsafe(true)}

// BenchmarkPatchOneField is a one-field write folded onto a set of n entities: what a
// watcher on the set pays per commit -- the store's step, which hands its state over
// (PatchOwned, as api.StepState does) -- and what a caller of the public Patch pays, with
// its defaults.
func BenchmarkPatchOneField(b *testing.B) {
	for _, n := range []int{200, 3000} {
		for _, store := range []bool{true, false} {
			b.Run(fmt.Sprintf("entities=%d/store=%v", n, store), func(b *testing.B) {
				doc, patch := entitySet(n), entityWrite(1, "ready")
				b.ReportAllocs()
				for b.Loop() {
					if store {
						next, err := PatchOwned(doc, patch, storeOpts...)
						if err != nil {
							b.Fatal(err)
						}
						doc = next
						continue
					}
					if _, err := Patch(doc, patch); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkFoldOntoSet folds 64 one-field writes to different entities, each onto the
// result of the last, the way the store steps: a watcher on the set stepping 64 commits.
func BenchmarkFoldOntoSet(b *testing.B) {
	for _, n := range []int{200, 3000} {
		b.Run(fmt.Sprintf("entities=%d/writes=64", n), func(b *testing.B) {
			cur := entitySet(n)
			writes := make([]*ir.Node, 64)
			for i := range writes {
				writes[i] = entityWrite(i*(n/64), "s"+strconv.Itoa(i))
			}
			b.ReportAllocs()
			for b.Loop() {
				for _, w := range writes {
					next, err := PatchOwned(cur, w, storeOpts...)
					if err != nil {
						b.Fatal(err)
					}
					cur = next
				}
			}
		})
	}
}
