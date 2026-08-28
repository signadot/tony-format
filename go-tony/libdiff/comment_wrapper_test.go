package libdiff

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// An operation belongs on the VALUE, and a head comment is a wrapper node AROUND the
// value. escaped tagged whatever node it was handed, so a value carrying a head
// comment came back with the operation on its wrapper -- where nothing sees it.
//
// mergeop walks past a comment before it looks for an operation, so the operation
// never ran: an !insert merged instead of replacing. And the log writes a comment as
// its lines and its child, so the tag was not even serialized: a !delete on a
// commented value reached the log AS the value, and replaying the delta put back what
// it was meant to remove (xqpvk3ehh12ks89mj5n0).
//
// Both faces of one defect, and both invisible in the rendering -- the encoder writes
// a tag on a wrapper exactly where it writes one on the value.
func TestEscapedTagsTheValueNotTheWrapper(t *testing.T) {
	value := ir.FromMap(map[string]*ir.Node{"k": ir.FromInt(1)})
	wrapped := ir.Comment(value, "# about k")

	got := escaped(wrapped, DeleteTag)

	if got.Type != ir.CommentType {
		t.Fatalf("the comment was dropped: got %s", got.Type)
	}
	if got.Tag != "" {
		t.Errorf("the wrapper carries %q, and an operation on a wrapper is seen by nothing", got.Tag)
	}
	if len(got.Values) != 1 {
		t.Fatalf("the wrapper holds %d values, want 1", len(got.Values))
	}
	inner := got.Values[0]
	if !ir.TagHas(inner.Tag, DeleteTag) {
		t.Errorf("the value carries %q, want it to carry %s", inner.Tag, DeleteTag)
	}
	if inner.Parent != got {
		t.Error("the value inside the wrapper is not parented to it")
	}
	// The lines are still what they were: this moves the operation, it does not
	// touch what was said about the value.
	if len(got.Lines) != 1 || got.Lines[0] != "# about k" {
		t.Errorf("the comment reads %v, want [# about k]", got.Lines)
	}
}

// The uncommented cases are untouched, including the two that are not a bare tag:
// an escape for a value that itself holds operations, and the argument form that
// carries the value's own tag.
func TestEscapedWithoutAComment(t *testing.T) {
	plain := escaped(ir.FromInt(1), DeleteTag)
	if plain.Tag != DeleteTag {
		t.Errorf("a plain value came back %q, want %s", plain.Tag, DeleteTag)
	}
	tagged := escaped(ir.FromInt(1).WithTag("!t1"), DeleteTag)
	if tagged.Tag != DeleteTag+"(t1)" {
		t.Errorf("a tagged value came back %q, want %s(t1)", tagged.Tag, DeleteTag)
	}
}
