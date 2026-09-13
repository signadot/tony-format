package tony

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// An array patch where the document holds no array introduces every element, each
// a patch applied to an absent document, as one past the end of a list is. It was
// returned as written, op markers and all: Patch({}, {b: [!insert 5]}) gave {b:
// [!insert 5]} (p478tacqh12krg32msn0 item 1; docs/matchpatch.md).
func TestArrayPatchOverNoArrayAppliesItsElements(t *testing.T) {
	for _, tc := range []struct{ doc, patch, want string }{
		{"{}", "{b: [!insert 5]}", "{b: [5]}"},
		{"{a: 1}", "{a: [!delete null, 2]}", "{a: [2]}"},
		{"{}", "{b: [!insert(t) 3, 4]}", "{b: [!t 3, 4]}"},
		{"{a: null}", "{a: [1, {x: !delete 1, y: 2}]}", "{a: [1, {y: 2}]}"},
	} {
		out, err := Patch(parseT(t, tc.doc), parseT(t, tc.patch))
		if err != nil {
			t.Fatalf("%s + %s: %v", tc.doc, tc.patch, err)
		}
		if d := Diff(plain(out), plain(parseT(t, tc.want))); d != nil {
			t.Errorf("%s + %s = %s, want %s", tc.doc, tc.patch, encodeT(t, out), tc.want)
		}
		if ir.TagHas(ir.Get(out, firstKey(out)).Tag, "!insert") {
			t.Errorf("%s + %s stored an op marker: %s", tc.doc, tc.patch, encodeT(t, out))
		}
	}
}

func firstKey(n *ir.Node) string { return n.Fields[0].String }

// plain is n without presentation tags, which say how a value was written and
// which Diff reads as a change.
func plain(n *ir.Node) *ir.Node {
	c := n.Clone()
	var walk func(*ir.Node)
	walk = func(x *ir.Node) {
		x.Tag = ir.StripPresentation(x.Tag)
		for _, v := range x.Values {
			walk(v)
		}
	}
	walk(c)
	return c
}
