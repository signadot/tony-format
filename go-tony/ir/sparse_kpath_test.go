package ir

import "testing"

// A sparse array is an OBJECT whose field keys are numbers -- there is no
// SparseArrayType -- so {7} names the value under the key 7, and not the seventh
// value.
//
// Both navigators had it wrong, in different ways and with different noise. get
// required an Array and then indexed it positionally, so it answered "expected
// array for sparse index, got Object" on the one shape it should have taken; and
// list dropped the segment along with every other non-field one, so it answered
// with NOTHING, which is the wrong answer worse than an error. The key is the
// field's own value, which is where index.indexPatchRec and extractTopLevelKPaths
// both take it from when they WRITE a {n} path.
func sparseDoc() *Node {
	// {v: {3: a, 7: b}}, the object keys being numbers
	inner := FromIntKeysMap(map[uint32]*Node{3: FromString("a"), 7: FromString("b")})
	return FromMap(map[string]*Node{"v": inner})
}

func TestSparseIndexNamesTheKeyNotThePosition(t *testing.T) {
	doc := sparseDoc()

	// 7 is the second value, so a positional reading answers the wrong thing or
	// nothing at all. This is the case that says which reading is in force.
	got, err := doc.GetKPath("v{7}")
	if err != nil {
		t.Fatalf("get v{7}: %v", err)
	}
	if got == nil || got.String != "b" {
		t.Errorf("get v{7} = %v, want b", got)
	}

	got, err = doc.GetKPath("v{3}")
	if err != nil {
		t.Fatalf("get v{3}: %v", err)
	}
	if got == nil || got.String != "a" {
		t.Errorf("get v{3} = %v, want a", got)
	}

	// A key the array does not hold is an absence, not a fault: nil and no error,
	// which is what the callers read as "nothing there".
	got, err = doc.GetKPath("v{9}")
	if err != nil {
		t.Errorf("get v{9}: %v", err)
	}
	if got != nil {
		t.Errorf("get v{9} = %v, want nothing", got)
	}
}

func TestSparseIndexInList(t *testing.T) {
	doc := sparseDoc()

	nodes, err := doc.ListKPath(nil, "v{7}")
	if err != nil {
		t.Fatalf("list v{7}: %v", err)
	}
	if len(nodes) != 1 || nodes[0].String != "b" {
		t.Errorf("list v{7} = %v, want [b]", nodes)
	}

	// {*} is every value the sparse array holds, in the order it holds them.
	nodes, err = doc.ListKPath(nil, "v{*}")
	if err != nil {
		t.Fatalf("list v{*}: %v", err)
	}
	if len(nodes) != 2 || nodes[0].String != "a" || nodes[1].String != "b" {
		t.Errorf("list v{*} = %v, want [a b]", nodes)
	}

	nodes, err = doc.ListKPath(nil, "v{9}")
	if err != nil {
		t.Errorf("list v{9}: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("list v{9} = %v, want nothing", nodes)
	}
}

// A segment which does not fit the node it meets is a non-match and not a fault,
// which is what lets a query walk nodes of every kind: `..x` visits leaves, arrays
// and objects alike, and a dense index means nothing at an object.
func TestAKindMismatchInListIsANonMatch(t *testing.T) {
	doc := sparseDoc()
	for _, q := range []string{"v[0]", "v[*]", "v(joe)"} {
		nodes, err := doc.ListKPath(nil, q)
		if err != nil {
			t.Errorf("list %s: %v, want no error", q, err)
		}
		if len(nodes) != 0 {
			t.Errorf("list %s = %v, want nothing", q, nodes)
		}
	}
}

// And a descent reaches one, which is the combination the CLI made reachable.
func TestDescendFindsASparseValue(t *testing.T) {
	doc := FromMap(map[string]*Node{
		"a": FromMap(map[string]*Node{
			"v": FromIntKeysMap(map[uint32]*Node{5: FromString("deep")}),
		}),
	})
	nodes, err := doc.ListKPath(nil, "..v{5}")
	if err != nil {
		t.Fatalf("list ..v{5}: %v", err)
	}
	if len(nodes) != 1 || nodes[0].String != "deep" {
		t.Errorf("list ..v{5} = %v, want [deep]", nodes)
	}
}
