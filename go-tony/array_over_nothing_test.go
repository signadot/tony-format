package tony

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
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

// An element with a head comment keeps its tag when it is introduced, where there
// was no list and past the end of one, with comments kept or not. Read off the
// comment wrapper the tag was "", so a commented `!t x` was stored as x, and a
// watch, which applies a patch without comments, saw a value a read did not
// (x72pp5j0h12kst3rwsn0).
func TestIntroducedElementKeepsItsTagUnderAHeadComment(t *testing.T) {
	for _, tc := range []struct{ doc, patch, want string }{
		{"{}", "{list: [a,\n # a note\n !t x]}", "{list: [a, !t x]}"},
		{"{}", "{list: !insert.raw [a,\n # a note\n !glob x]}", "{list: [a, !glob x]}"},
		{"{}", "{list: !insert.raw [\n # a note\n !not.or .[seen]]}", "{list: [!not.or .[seen]]}"},
		{"{list: [a]}", "{list: [a,\n # a note\n !t x]}", "{list: [a, !t x]}"},
		{"{list: [a]}", "{list: [a,\n # a note\n !delete null]}", "{list: [a]}"},
		{"{}", "{list: [a, !t x]}", "{list: [a, !t x]}"},
	} {
		for _, comments := range []bool{false, true} {
			p, err := parse.Parse([]byte(tc.patch), parse.ParseComments(true))
			if err != nil {
				t.Fatal(err)
			}
			out, err := Patch(parseT(t, tc.doc), p, mergeop.Comments(comments))
			if err != nil {
				t.Fatalf("%s + %s: %v", tc.doc, tc.patch, err)
			}
			got := ir.Uncomment(out).Clone()
			if comments {
				got = ir.StripComments(got)
			}
			if d := Diff(plain(got), plain(parseT(t, tc.want))); d != nil {
				t.Errorf("comments %v: %s + %s = %s, want %s", comments, tc.doc, tc.patch, encodeT(t, got), tc.want)
			}
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
