package ir

import (
	"slices"
	"testing"
)

// A comment does not wrap a comment: a value has one set of preceding comments,
// and they compose as lines. Given a comment, Comment appended c to it unmarked and
// then wrapped it anyway, so the text was there twice -- bare inside, marked
// outside (addsgv1yh12kszdxmdn0).
func TestCommentOnACommentAddsLines(t *testing.T) {
	v := FromString("x")
	once := Comment(v, "first")
	twice := Comment(once, "second\n# third")
	if twice != once {
		t.Fatalf("Comment wrapped a comment: got %s node holding %s, want the comment it was given",
			twice.Type, twice.Values[0].Type)
	}
	if want := []string{"# first", "# second", "# third"}; !slices.Equal(twice.Lines, want) {
		t.Errorf("lines %q, want %q", twice.Lines, want)
	}
	if len(twice.Values) != 1 || twice.Values[0] != v || v.Parent != twice {
		t.Errorf("the comment no longer holds the value it was put on")
	}
}
