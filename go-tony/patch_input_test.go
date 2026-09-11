package tony

import (
	"fmt"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
)

// linksHold says whether every node beneath n is linked as the child it is: its Parent is
// the node holding it, at the index it holds it, and a line comment's Parent is the node
// it annotates. It answers the first link that does not hold.
func linksHold(n *ir.Node) error {
	var walk func(n *ir.Node, at string) error
	walk = func(n *ir.Node, at string) error {
		for i, f := range n.Fields {
			if f.Parent != n || f.ParentIndex != i {
				return fmt.Errorf("key %d of %s links to %p at %d, not to its object %p",
					i, at, f.Parent, f.ParentIndex, n)
			}
		}
		for i, v := range n.Values {
			if v.Parent != n || v.ParentIndex != i {
				return fmt.Errorf("value %d of %s links to %p at %d, not to its container %p",
					i, at, v.Parent, v.ParentIndex, n)
			}
			if n.Type == ir.ObjectType && i < len(n.Fields) && n.Fields[i].Type == ir.StringType &&
				v.ParentField != n.Fields[i].String {
				return fmt.Errorf("value %d of %s says it is field %q, held as %q",
					i, at, v.ParentField, n.Fields[i].String)
			}
			if err := walk(v, fmt.Sprintf("%s/%d", at, i)); err != nil {
				return err
			}
		}
		if n.Comment != nil {
			if n.Comment.Parent != n {
				return fmt.Errorf("the line comment of %s links to %p, not to %p", at, n.Comment.Parent, n)
			}
			if err := walk(n.Comment, at+"#"); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(n, "$")
}

// nodesOf is every node reachable from n: keys, values and comments.
func nodesOf(n *ir.Node) map[*ir.Node]bool {
	seen := map[*ir.Node]bool{}
	var walk func(*ir.Node)
	walk = func(n *ir.Node) {
		if n == nil || seen[n] {
			return
		}
		seen[n] = true
		for _, f := range n.Fields {
			walk(f)
		}
		for _, v := range n.Values {
			walk(v)
		}
		walk(n.Comment)
	}
	walk(n)
	return seen
}

// patchShapes reaches every way a merge builds its result: the object merge's fast and
// general paths, arrays by position, and the operations that carry the document's
// subtrees across.
var patchShapes = []struct {
	name, doc, patch string
	comments         bool
}{
	{"an untouched sibling", "{a: 1, b: {c: 2}}", "{a: 3}", false},
	{"a key added before the others", "{b: {x: 1}, c: {y: 2}}", "{a: 0}", false},
	{"a field deleted", "{a: 1, b: {c: 2}}", "{a: !delete 1}", false},
	{"a nested merge", "{a: {b: {c: 1}, d: {e: 2}}, f: {g: 3}}", "{a: {b: {c: 9}}}", false},
	{"the store's options", "{a: 1, b: {c: 2}}", "{a: 3}", true},
	{"a document not in key order", "{b: {x: 1}, a: 1}", "{a: 2}", false},
	{"an array patched by position", "{l: [{a: 1}, {b: {c: 2}}]}", "{l: [{a: 2}, {b: {c: 2}}]}", false},
	{"an arraydiff", "{l: [{a: 1}, {b: {c: 2}}]}", "{l: !arraydiff {0: !replace {from: {a: 1}, to: {a: 9}}}}", false},
	{"a keyed list", "{l: !key(id) [{id: a, v: {x: 1}}, {id: b, v: {x: 2}}]}", "{l: !key(id) [{id: a, v: {x: 9}}]}", false},
	{"a rename", "{a: {x: 1}, b: {y: 2}}", "!rename\n- from: a\n  to: c", false},
	{"a field op", "{a: {x: 1}, b: {y: 2}}", "!field(a,c) null", false},
	{"an addtag", "{a: {x: 1}, b: {y: 2}}", "{a: !addtag(t) null}", false},
	{"a nullify", "{spec: {a: 1}, k: 1}", "{spec: !nullify null}", false},
	{"a nullify of a tagged value", "{spec: !t {a: 1}, k: 1}", "{spec: !nullify null}", true},
	{"a pass", "{a: {x: 1}, b: {y: 2}}", "{a: !pass null}", false},
	{"comments kept", "# head\na: 1 # line\nb: # note\n  c: 2 # deep\n", "{a: 2}", true},
	{"a comment op", "# head\na: 1\nb: {c: 2}\n", "{b: !comment {head: [\"# new\"]}}", true},
}

// Patch leaves the document it is given as it found it -- the same content and the same
// links -- and answers a document of its own. The two share no node, because a node has
// one Parent and cannot name both of them: the result used to hold the document's
// untouched subtrees and re-parent them, so the document's own children named the result
// and an operator walking up from them read the wrong document (qzmkhfjqh12ksydsmdn0).
// !nullify went further and rewrote the node it was given (fk1vg9sxh12ksyxxmdn0).
func TestPatchLeavesItsInputAlone(t *testing.T) {
	for _, test := range patchShapes {
		t.Run(test.name, func(t *testing.T) {
			doc, err := parse.Parse([]byte(test.doc), parse.ParseComments(test.comments))
			if err != nil {
				t.Fatalf("parse doc: %v", err)
			}
			if err := linksHold(doc); err != nil {
				t.Fatalf("the parsed document's links do not hold before the patch: %v", err)
			}
			patch, err := parse.Parse([]byte(test.patch), parse.ParseComments(test.comments))
			if err != nil {
				t.Fatalf("parse patch: %v", err)
			}
			before := doc.Clone()
			res, err := Patch(doc, patch, mergeop.Comments(test.comments))
			if err != nil {
				t.Fatalf("patch: %v", err)
			}
			if !doc.DeepEqualWithComments(before) {
				t.Errorf("the document changed:\nwas %s\nnow %s", encode.MustString(before), encode.MustString(doc))
			}
			if err := linksHold(doc); err != nil {
				t.Errorf("the document's links: %v", err)
			}
			if res == nil {
				return
			}
			if err := linksHold(res); err != nil {
				t.Errorf("the result's links: %v", err)
			}
			in := nodesOf(doc)
			for n := range nodesOf(res) {
				if in[n] {
					t.Errorf("the result holds a node of the document: %s", encode.MustString(n))
					break
				}
			}
		})
	}
}

// Two patches of one document answer two documents, and a question the second asks of its
// root is answered by it, not by the first: the shared subtree named whichever result was
// built last, so !get-path(root) read the other one (qzmkhfjqh12ksydsmdn0).
func TestPatchAnswersFromTheDocumentItWasGiven(t *testing.T) {
	p := func(s string) *ir.Node {
		n, err := parse.Parse([]byte(s))
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return n
	}
	doc := p("{spec: {v: 1}, status: {}}")
	res1, err := Patch(doc, p("{spec: {v: 10}}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Patch(doc, p("{spec: {v: 20}}")); err != nil {
		t.Fatal(err)
	}
	out, err := Patch(res1, p("{status: {y: !get-path(root) spec.v}}"))
	if err != nil {
		t.Fatal(err)
	}
	y, err := out.GetKPath("status.y")
	if err != nil || y == nil || y.Int64 == nil || *y.Int64 != 10 {
		t.Errorf("status.y = %v (err %v), want 10, res1's own spec.v", y, err)
	}
}

// PatchOwned takes the document over instead of copying it, and what it answers is a
// document linked as one: every node the child of the node that holds it, the keys it
// carries across included. The document it was handed is not examined -- its links lead
// into the result, which is the contract.
func TestPatchOwnedAnswersALinkedDocument(t *testing.T) {
	for _, test := range patchShapes {
		t.Run(test.name, func(t *testing.T) {
			doc, err := parse.Parse([]byte(test.doc), parse.ParseComments(test.comments))
			if err != nil {
				t.Fatalf("parse doc: %v", err)
			}
			patch, err := parse.Parse([]byte(test.patch), parse.ParseComments(test.comments))
			if err != nil {
				t.Fatalf("parse patch: %v", err)
			}
			want, err := Patch(doc, patch, mergeop.Comments(test.comments))
			if err != nil {
				t.Fatalf("patch: %v", err)
			}
			res, err := PatchOwned(doc, patch, mergeop.Comments(test.comments))
			if err != nil {
				t.Fatalf("PatchOwned: %v", err)
			}
			if !res.DeepEqualWithComments(want) {
				t.Errorf("PatchOwned answers %s, Patch %s", encode.MustString(res), encode.MustString(want))
			}
			if err := linksHold(res); err != nil {
				t.Errorf("the result's links: %v", err)
			}
		})
	}
}
