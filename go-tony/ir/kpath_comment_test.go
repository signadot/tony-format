package ir

import (
	"testing"
)

// TestListKPathThroughComments: getKPath and listPath were taught to see through
// a comment wrapper; listKPath was missed, and it is the walk logd's wildcards
// go through. It switched on the node's type, found a Comment where it wanted a
// container, and answered with NOTHING and no error -- the one shape of this bug
// that says nothing at all (3cdjz00jh12krns4g1n0).
func TestListKPathThroughComments(t *testing.T) {
	// Built rather than parsed: this package cannot import parse.
	elem := &Node{Type: ObjectType,
		Fields: []*Node{{Type: StringType, String: "name"}},
		Values: []*Node{{Type: StringType, String: "Alice"}},
	}
	arr := &Node{Type: ArrayType, Values: []*Node{elem}}
	commented := func(n *Node) *Node {
		return &Node{Type: CommentType, Lines: []string{"# note"}, Values: []*Node{n}}
	}
	obj := func(v *Node) *Node {
		return &Node{Type: ObjectType,
			Fields: []*Node{{Type: StringType, String: "users"}},
			Values: []*Node{v},
		}
	}

	for _, tc := range []struct {
		name string
		doc  *Node
	}{
		{"no comment", obj(arr)},
		{"a comment above the document", commented(obj(arr))},
		{"a comment above the array", obj(commented(arr))},
		{"a comment above the element", obj(&Node{Type: ArrayType, Values: []*Node{commented(elem)}})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.doc.ListKPath(nil, "users[*].name")
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].String != "Alice" {
				t.Fatalf("the walk answered with %d nodes, and the path names one", len(got))
			}
		})
	}
}

// TestListKPathWithComments: the option says what to do with the comment on the
// node the walk ANSWERS with, as it does for ListPathWith. Seeing through one on
// the way is not optional.
func TestListKPathWithComments(t *testing.T) {
	inner := &Node{Type: NumberType, Number: "1"}
	doc := &Node{Type: ObjectType,
		Fields: []*Node{{Type: StringType, String: "a"}},
		Values: []*Node{{Type: CommentType, Lines: []string{"# note"}, Values: []*Node{inner}}},
	}
	kept, err := doc.ListKPathWith(nil, "a", WithComments(true))
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].Type != CommentType {
		t.Fatalf("WithComments(true) answered with %v", kept)
	}
	plain, err := doc.ListKPathWith(nil, "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 1 || plain[0].Type != NumberType {
		t.Fatalf("the default answered with %v, and a path names the value", plain)
	}
}
