package ir

import "testing"

// StripComments answers a stripped tree and leaves the one it was given alone.
//
// It used to strip IN PLACE, and its caller strips the RESULT of a patch, which
// shares its untouched subtrees with the document that was patched. So a patch
// with comments off reached back into the caller's document and took the comments
// out of it -- the property head.go names and relies on ("it does not mutate the
// document it is given, so an earlier head stays valid for anyone still holding
// it"), true only because logd passes Comments(true) and so never reached here.
func TestStripCommentsLeavesItsInputAlone(t *testing.T) {
	doc := FromMap(map[string]*Node{
		"a": FromInt(1),
		"b": FromMap(map[string]*Node{"c": FromInt(2)}),
	})
	// a line comment on a leaf, and a wrapper around a subtree
	Get(doc, "a").Comment = &Node{Type: CommentType, Lines: []string{"# about a"}}
	before := countComments(doc)
	if before == 0 {
		t.Fatal("the fixture has no comments to lose")
	}

	stripped := StripComments(doc)

	if got := countComments(doc); got != before {
		t.Errorf("the input lost comments: %d -> %d", before, got)
	}
	if got := countComments(stripped); got != 0 {
		t.Errorf("the answer kept %d comments", got)
	}
	// The answer is a tree in its own right, not a view onto the input.
	if Get(stripped, "a") == Get(doc, "a") {
		t.Error("the answer shares nodes with the input")
	}
	// and it is still the same value otherwise
	if !stripped.DeepEqual(doc) {
		t.Error("stripping comments changed the value")
	}
}

func countComments(n *Node) int {
	if n == nil {
		return 0
	}
	c := 0
	if n.Type == CommentType {
		c++
	}
	if n.Comment != nil {
		c++
	}
	for _, v := range n.Values {
		c += countComments(v)
	}
	for _, f := range n.Fields {
		c += countComments(f)
	}
	return c
}
