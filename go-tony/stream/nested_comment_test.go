package stream

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// TestNestedHeadCommentIsRefused: a comment node holding a comment node is not
// IR (docs/ir.md), and the stream could not carry one if it were -- two wrappers
// and one wrapper of two lines are the same pair of events, so what went in
// could not come back. It is refused on the way out rather than written and lost
// (3cdjz00jh12krns4g1n0).
func TestNestedHeadCommentIsRefused(t *testing.T) {
	inner := &ir.Node{Type: ir.CommentType, Lines: []string{"# inner"},
		Values: []*ir.Node{ir.FromInt(1)}}
	doc := &ir.Node{Type: ir.CommentType, Lines: []string{"# outer"},
		Values: []*ir.Node{inner}}

	_, err := NodeToEvents(doc)
	if err == nil {
		t.Fatal("a nested head comment was written to the stream")
	}
	if !strings.Contains(err.Error(), "head comment") {
		t.Errorf("the error does not say what is wrong: %v", err)
	}
}

// TestTwoHeadCommentEventsAreRefused: the same shape arriving from a stream
// written by something else. Merging them silently would invent an association
// the writer did not make.
func TestTwoHeadCommentEventsAreRefused(t *testing.T) {
	evs := []Event{
		{Type: EventHeadComment, CommentLines: []string{"# one"}},
		{Type: EventHeadComment, CommentLines: []string{"# two"}},
		{Type: EventInt, Int: 1},
	}
	if _, err := EventsToNode(evs); err == nil {
		t.Fatal("two head comments before one value were accepted")
	}
}

// TestOneHeadCommentOfManyLines is the shape that IS legal, and the one the
// parser produces for comments written in two places before one value.
func TestOneHeadCommentOfManyLines(t *testing.T) {
	evs := []Event{
		{Type: EventHeadComment, CommentLines: []string{"# one", "# two"}},
		{Type: EventInt, Int: 1},
	}
	n, err := EventsToNode(evs)
	if err != nil {
		t.Fatal(err)
	}
	if n.Type != ir.CommentType || len(n.Lines) != 2 {
		t.Fatalf("got %s with lines %v", n.Type, n.Lines)
	}
	if ir.Uncomment(n).Type != ir.NumberType {
		t.Fatalf("the value under the comment is %s", ir.Uncomment(n).Type)
	}
}
