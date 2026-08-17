package ir

import "testing"

// A hash is only usable as an identity key if it answers for an equality, and
// this one answered for neither: it left the TAG out, which every equality here
// counts, and it counts comments, which DeepEqual does not. So {a: 1} and
// !delete {a: 1} -- a value and its own deletion -- hashed the same.
//
// It is the hash of DeepEqualWithComments, and these are the two directions that
// makes true: equal implies same hash, and the differences no equality overlooks
// are differences it can see.
func TestHashAnswersForDeepEqualWithComments(t *testing.T) {
	mk := func() *Node { return FromMap(map[string]*Node{"a": FromInt(1)}) }

	t.Run("a tag is part of what a node is", func(t *testing.T) {
		x, y := mk(), mk()
		y.Tag = "!delete"
		if x.DeepEqual(y) {
			t.Fatal("a tagged node equals an untagged one")
		}
		if x.Hash() == y.Hash() {
			t.Error("a value and its deletion hash the same")
		}
	})

	t.Run("equal documents hash equal", func(t *testing.T) {
		x, y := mk(), mk()
		if !x.DeepEqualWithComments(y) {
			t.Fatal("two of the same document are not equal")
		}
		if x.Hash() != y.Hash() {
			t.Error("equal documents hash differently")
		}
	})

	t.Run("a comment is part of the document, which is the question this asks", func(t *testing.T) {
		x, y := mk(), mk()
		y.Comment = &Node{Type: CommentType, Lines: []string{"# note"}}
		if x.DeepEqualWithComments(y) {
			t.Fatal("a commented document equals an uncommented one")
		}
		if x.Hash() == y.Hash() {
			t.Error("a comment left no trace in the hash")
		}
	})
}

// Compare asks about VALUES, so it sees through the comment that describes one.
// Every commented value used to rank first and compare equal to every other
// commented value, which a sort reads as a tie between different things.
func TestCompareSeesThroughComments(t *testing.T) {
	commented := func(n *Node) *Node {
		return &Node{Type: CommentType, Lines: []string{"# c"}, Values: []*Node{n}}
	}

	if got := Compare(commented(FromInt(1)), commented(FromString("zzz"))); got == 0 {
		t.Error("1 and \"zzz\" compare equal when both are commented")
	}
	if got := Compare(commented(FromInt(1)), FromInt(1)); got != 0 {
		t.Errorf("a commented 1 and a bare 1 compare %d, want 0", got)
	}
	if got := Compare(commented(FromInt(2)), FromInt(1)); got <= 0 {
		t.Errorf("a commented 2 compares %d against 1, want greater", got)
	}
}

// Truth reads a node as a condition, and a comment is what was said about a
// value rather than part of it.
func TestTruthSeesThroughComments(t *testing.T) {
	commented := func(n *Node) *Node {
		return &Node{Type: CommentType, Lines: []string{"# c"}, Values: []*Node{n}}
	}
	for _, tc := range []struct {
		name string
		node *Node
		want bool
	}{
		{"true", FromBool(true), true},
		{"a commented true", commented(FromBool(true)), true},
		{"a commented false", commented(FromBool(false)), false},
		{"a commented empty string", commented(FromString("")), false},
		{"a commented non-empty string", commented(FromString("x")), true},
		{"nothing at all", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Truth(tc.node); got != tc.want {
				t.Errorf("Truth = %v, want %v", got, tc.want)
			}
		})
	}
}
