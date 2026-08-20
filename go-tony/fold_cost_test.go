package tony

import (
	"strconv"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
)

// Folding a one-field patch onto a document must not allocate a node per field of that
// document. It did: the object merge built a fresh key node for every key it carried
// through, so a write to one entity in a set of three thousand allocated three thousand
// key nodes -- on every write, and again in every watcher stepping the same commit
// (rkb7p8v5h12ksdnmgsn0).
//
// Allocations rather than time: the property is that the work does not scale with the
// document, and a count says that on any machine, in any weather.
func TestFoldDoesNotAllocatePerField(t *testing.T) {
	fold := func(entities int) float64 {
		set := map[string]*ir.Node{}
		for i := 0; i < entities; i++ {
			id := "e" + strconv.Itoa(i)
			set[id] = ir.FromMap(map[string]*ir.Node{"id": ir.FromString(id)})
		}
		doc := ir.FromMap(map[string]*ir.Node{
			"verse": ir.FromMap(map[string]*ir.Node{"entities": ir.FromMap(set)}),
		})
		patch := ir.FromMap(map[string]*ir.Node{
			"verse": ir.FromMap(map[string]*ir.Node{
				"entities": ir.FromMap(map[string]*ir.Node{
					"e1": ir.FromMap(map[string]*ir.Node{"status": ir.FromString("ready")}),
				}),
			}),
		})
		return testing.AllocsPerRun(20, func() {
			// The options the store folds with: comments are kept, because a store
			// keeps what it is given.
			if _, err := Patch(doc, patch, mergeop.Comments(true)); err != nil {
				t.Fatalf("fold: %s", err)
			}
		})
	}

	small, large := fold(200), fold(3000)
	t.Logf("allocations for a one-field patch: %.0f at 200 entities, %.0f at 3000", small, large)

	// Fifteen times the document, and the fold may allocate a little more -- the slices
	// it copies are larger -- but not a node per field. Twice is far above the slice
	// growth and far below the 15x a per-field allocation would give.
	if large > 2*small {
		t.Errorf("a fold allocates %.0f at 3000 entities against %.0f at 200: it is building a node per field",
			large, small)
	}
}

// The same, through the public Patch with its default options -- which strip comments from
// the result. Stripping cost a deep clone of the whole tree whether or not there was a
// comment in it, so every caller outside the store paid a node per node
// (rkb7p8v5h12ksdnmgsn0).
func TestStrippingCommentsCostsNothingWhenThereAreNone(t *testing.T) {
	build := func(entities int) (doc, patch *ir.Node) {
		set := map[string]*ir.Node{}
		for i := 0; i < entities; i++ {
			id := "e" + strconv.Itoa(i)
			set[id] = ir.FromMap(map[string]*ir.Node{"id": ir.FromString(id)})
		}
		doc = ir.FromMap(map[string]*ir.Node{"entities": ir.FromMap(set)})
		patch = ir.FromMap(map[string]*ir.Node{
			"entities": ir.FromMap(map[string]*ir.Node{
				"e1": ir.FromMap(map[string]*ir.Node{"status": ir.FromString("ready")}),
			}),
		})
		return doc, patch
	}
	allocs := func(entities int) float64 {
		doc, patch := build(entities)
		return testing.AllocsPerRun(20, func() {
			if _, err := Patch(doc, patch); err != nil {
				t.Fatalf("patch: %s", err)
			}
		})
	}
	small, large := allocs(200), allocs(3000)
	t.Logf("allocations for a one-field patch through Patch: %.0f at 200 entities, %.0f at 3000", small, large)
	if large > 2*small {
		t.Errorf("Patch allocates %.0f at 3000 entities against %.0f at 200: the strip is cloning the document",
			large, small)
	}

	// And a document which DOES carry a comment still comes back without it: the scan
	// decides whether to clone, not whether to strip.
	doc, patch := build(2)
	doc.Values[0].Values[0].Comment = ir.FromString("a note")
	out, err := Patch(doc, patch)
	if err != nil {
		t.Fatalf("patch: %s", err)
	}
	var walk func(*ir.Node) bool
	walk = func(n *ir.Node) bool {
		if n == nil {
			return false
		}
		if n.Comment != nil || n.Type == ir.CommentType {
			return true
		}
		for _, f := range n.Fields {
			if walk(f) {
				return true
			}
		}
		for _, v := range n.Values {
			if walk(v) {
				return true
			}
		}
		return false
	}
	if walk(out) {
		t.Error("a comment survived a patch which strips them")
	}
}
