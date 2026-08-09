package libdiff

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func mustParse(t *testing.T, s string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(s))
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return n
}

// stubDiff stands in for tony.doDiff, which libdiff cannot import. It reproduces the
// one property this file depends on -- equal nodes diff to nil -- and otherwise
// dispatches the way the real one does.
func stubDiff(from, to *ir.Node) *ir.Node {
	if from.DeepEqual(to) {
		return nil
	}
	if from.Type == ir.ObjectType && to.Type == ir.ObjectType {
		return DiffObject(from, to, stubDiff)
	}
	return MakeDiff(from, to)
}

// TestDiffArrayByKey_Equal covers two keyed lists holding the same elements. df reports
// that as nil, which the result loop used to index straight into a panic.
func TestDiffArrayByKey_Equal(t *testing.T) {
	from := mustParse(t, `[{name: "a", v: 1}, {name: "b", v: 2}]`)
	to := mustParse(t, `[{name: "a", v: 1}, {name: "b", v: 2}]`)

	res, err := DiffArrayByKey(from, to, "name", stubDiff)
	if err != nil {
		t.Fatalf("DiffArrayByKey: %v", err)
	}
	if res != nil {
		t.Errorf("expected no diff between equal keyed lists, got %s", res.Type)
	}
}

// TestDiffArrayByKey_EqualElementsDifferentOrder: a keyed list is identified by key, not
// by position, so a reordering is not a difference.
func TestDiffArrayByKey_EqualElementsDifferentOrder(t *testing.T) {
	from := mustParse(t, `[{name: "a", v: 1}, {name: "b", v: 2}]`)
	to := mustParse(t, `[{name: "b", v: 2}, {name: "a", v: 1}]`)

	res, err := DiffArrayByKey(from, to, "name", stubDiff)
	if err != nil {
		t.Fatalf("DiffArrayByKey: %v", err)
	}
	if res != nil {
		t.Errorf("expected no diff for a reordering of the same keyed elements, got %s", res.Type)
	}
}

// TestDiffArrayByKey_Changed keeps the non-empty path honest alongside the nil fix.
func TestDiffArrayByKey_Changed(t *testing.T) {
	from := mustParse(t, `[{name: "a", v: 1}]`)
	to := mustParse(t, `[{name: "a", v: 2}]`)

	res, err := DiffArrayByKey(from, to, "name", stubDiff)
	if err != nil {
		t.Fatalf("DiffArrayByKey: %v", err)
	}
	if res == nil {
		t.Fatal("expected a diff when a keyed element's value changed")
	}
	if res.Type != ir.ArrayType || len(res.Values) != 1 {
		t.Fatalf("expected a one-element array diff, got %s", res.Type)
	}
}
