package parse

import (
	"slices"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// nestedComment reports the path of the first comment node holding another,
// which docs/ir.md says cannot exist: a comment node's single value is a
// NON-comment node.
func nestedComment(n *ir.Node) *ir.Node {
	if n == nil {
		return nil
	}
	if n.Type == ir.CommentType {
		for _, v := range n.Values {
			if v.Type == ir.CommentType {
				return n
			}
		}
	}
	for _, v := range n.Values {
		if bad := nestedComment(v); bad != nil {
			return bad
		}
	}
	return nil
}

// TestHeadCommentsDoNotNest: a value can be preceded by comments written above
// its key and above the first line of its block, and the association rule gives
// both to the same value -- the next one to begin. Each was wrapping it in turn,
// making a comment node holding a comment node, which the IR does not have.
//
// Everything that met one kept the inner comment and dropped the outer: the
// event stream writes the same two events for one wrapper of two lines and for
// two wrappers, so what went in could not come back (3cdjz00jh12krns4g1n0).
func TestHeadCommentsDoNotNest(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      []string
	}{
		{"above the key and above the block", "# above the spec\nspec:\n  # above replicas\n  replicas: 3\n",
			[]string{"# above the spec", "# above replicas"}},
		{"three places at once", "# one\nspec:\n  # two\n  # three\n  replicas: 3\n",
			[]string{"# one", "# two", "# three"}},
		{"above a list", "# above the key\nitems:\n  # above the first\n- a\n",
			[]string{"# above the key", "# above the first"}},
		{"a brace object", "# above the key\nspec: {\n  # inside\n  a: 1}\n",
			[]string{"# above the key", "# inside"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := Parse([]byte(tc.src), ParseComments(true))
			if err != nil {
				t.Fatal(err)
			}
			if bad := nestedComment(n); bad != nil {
				t.Fatalf("a comment node holds a comment node, with lines %v", bad.Lines)
			}
			// and the lines are all there, in writing order
			var found []string
			var walk func(*ir.Node)
			walk = func(x *ir.Node) {
				if x == nil {
					return
				}
				if x.Type == ir.CommentType {
					found = append(found, x.Lines...)
				}
				for _, v := range x.Values {
					walk(v)
				}
			}
			walk(n)
			for _, want := range tc.want {
				if !slices.Contains(found, want) {
					t.Errorf("the parse lost %q, keeping %v", want, found)
				}
			}
		})
	}
}

// TestLatchDoesNotSwallowHeadComments: a field line can carry a line comment --
// "spec: # latch" -- and the block under it can then open with head comments of
// its own. Both kinds precede the value, and they were taken once each in a
// fixed order, so whatever followed the latch was left for the skip loop to
// discard: a comment written inside a block vanished whenever the field line
// also carried one (3cdjz00jh12krns4g1n0).
func TestLatchDoesNotSwallowHeadComments(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      []string
	}{
		{"a latch then a head comment", "spec: # latch\n  # about\n  replicas: 3\n",
			[]string{"# latch", "# about"}},
		{"a latch then two", "spec: # latch\n  # one\n  # two\n  replicas: 3\n",
			[]string{"# latch", "# one", "# two"}},
		{"a head comment, a latch, and another", "# before\nspec: # latch\n  # about\n  replicas: 3\n",
			[]string{"# before", "# latch", "# about"}},
		{"a latch on a list", "items: # latch\n  # about\n- a\n",
			[]string{"# latch", "# about"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := Parse([]byte(tc.src), ParseComments(true))
			if err != nil {
				t.Fatal(err)
			}
			if bad := nestedComment(n); bad != nil {
				t.Fatalf("a comment node holds a comment node, lines %v", bad.Lines)
			}
			var found []string
			var walk func(*ir.Node)
			walk = func(x *ir.Node) {
				if x == nil {
					return
				}
				found = append(found, x.Lines...)
				if x.Comment != nil {
					found = append(found, x.Comment.Lines...)
				}
				for _, v := range x.Values {
					walk(v)
				}
			}
			walk(n)
			for _, want := range tc.want {
				if !slices.ContainsFunc(found, func(l string) bool { return strings.Contains(l, want) }) {
					t.Errorf("the parse lost %q, keeping %q", want, found)
				}
			}
		})
	}
}
