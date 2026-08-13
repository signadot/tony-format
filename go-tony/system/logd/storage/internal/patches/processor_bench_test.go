package patches

import (
	"fmt"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// benchPatches builds a delta-log range in the shape a long-lived store accumulates:
// many entries, each writing one entity deep under one of a handful of slices, plus
// the occasional write at a slice root that dominates everything under it.
func benchPatches(entries, slices int) []*ir.Node {
	out := make([]*ir.Node, 0, entries)
	for i := 0; i < entries; i++ {
		slice := fmt.Sprintf("slice%d", i%slices)
		if i%97 == 0 {
			// A write at the slice root: dominates every entity write under it, so
			// those entries must be navigated to this path rather than dropped.
			out = append(out, ir.FromMap(map[string]*ir.Node{
				"verse": ir.FromMap(map[string]*ir.Node{
					slice: ir.FromMap(map[string]*ir.Node{
						"generation": ir.FromInt(int64(i)),
					}).WithTag(tx.PatchRootTag),
				}),
			}))
			continue
		}
		entity := fmt.Sprintf("entity%d", i%280)
		out = append(out, ir.FromMap(map[string]*ir.Node{
			"verse": ir.FromMap(map[string]*ir.Node{
				slice: ir.FromMap(map[string]*ir.Node{
					entity: ir.FromMap(map[string]*ir.Node{
						"status": ir.FromString("ready"),
						"commit": ir.FromInt(int64(i)),
					}).WithTag(tx.PatchRootTag),
				}),
			}),
		}))
	}
	return out
}

func BenchmarkBuildPatchValueIndex(b *testing.B) {
	for _, entries := range []int{100, 1000, 5000} {
		patches := benchPatches(entries, 18)
		b.Run(fmt.Sprintf("entries=%d", entries), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := buildPatchValueIndex(patches); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
