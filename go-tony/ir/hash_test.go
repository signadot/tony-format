package ir

import "testing"

// mkNode builds a small nested document, freshly each call, so tests can compare
// hashes of independently-constructed but structurally-equal nodes.
func mkNode() *Node {
	return FromMap(map[string]*Node{
		"s": FromString("x"),
		"n": FromInt(42),
		"a": FromSlice([]*Node{FromString("a"), FromInt(1), Null()}),
		"b": FromBool(true),
	})
}

// TestHash_DeterministicAcrossCalls guards issue f69agjyeh12ks item 16: Hash must
// be stable — the same node hashes identically on repeated calls (previously each
// call took a fresh random maphash seed, so it drifted every time).
func TestHash_DeterministicAcrossCalls(t *testing.T) {
	n := mkNode()
	first := n.Hash()
	for i := 0; i < 20; i++ {
		if got := n.Hash(); got != first {
			t.Fatalf("call %d differs: %d != %d — Hash is not deterministic", i, got, first)
		}
	}
}

// TestHash_StructurallyEqualNodesHashEqual: two independently built, equal nodes
// must hash equal (it is a content hash, usable for identity/dedup).
func TestHash_StructurallyEqualNodesHashEqual(t *testing.T) {
	if a, b := mkNode().Hash(), mkNode().Hash(); a != b {
		t.Fatalf("equal nodes hash differently: %d != %d", a, b)
	}
}

// TestHash_DistinguishesContent: different values/structure hash differently.
func TestHash_DistinguishesContent(t *testing.T) {
	cases := [][2]*Node{
		{FromString("a"), FromString("b")},
		{FromInt(1), FromInt(2)},
		{FromString("1"), FromInt(1)}, // type matters
		{FromSlice([]*Node{FromInt(1), FromInt(2)}), FromSlice([]*Node{FromInt(2), FromInt(1)})}, // order matters
		{FromMap(map[string]*Node{"a": FromInt(1)}), FromMap(map[string]*Node{"b": FromInt(1)})}, // key matters
	}
	for i, c := range cases {
		if c[0].Hash() == c[1].Hash() {
			t.Errorf("case %d: distinct nodes collide", i)
		}
	}
}

// TestHash_IncludesComments guards the second bug: the comment hash was computed
// but never written into the hasher, so "Hash includes comments" was false.
func TestHash_IncludesComments(t *testing.T) {
	plain := FromString("x")
	commented := Comment(FromString("x"), "a note")
	if plain.Hash() == commented.Hash() {
		t.Fatal("a comment does not affect the hash, but the doc says it should")
	}
	// still deterministic with a comment attached
	if commented.Hash() != commented.Hash() {
		t.Fatal("commented node hash is not deterministic")
	}
}
