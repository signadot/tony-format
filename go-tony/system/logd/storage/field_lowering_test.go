package storage

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// !field(from,to) is a relative operation -- it renames whatever is at from -- so the log
// keeps its RESULT: a delete of the old field and the value under the new one, which
// applies the same against any base. Storing the operation as written would rename a
// field a later write had put back.
//
// It needs the operation not to mutate the document it is applied to: verifyApplies
// applies it to the kept head, and a rename done in place makes base and next one object,
// so the diff of the two is empty and the write is "kept as sent" -- the relative
// operation in the log, and the head rewritten under every reader sharing it.
func TestFieldRenameIsStoredAsItsResult(t *testing.T) {
	s := openTestStorage(t)
	c1 := mustCommit(t, s, nil, `{a: {x: 1, y: 2}}`)
	c2 := mustCommit(t, s, nil, `{a: !field(x,z) null}`)

	after, err := readStateAt(s, "", c2, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := after.GetKPath("a")
	if ir.Get(a, "x") != nil || ir.Get(a, "z") == nil || ir.Get(a, "y") == nil {
		t.Fatalf("after the rename a is %s, want {y: 2, z: 1}", mustEncode(t, a))
	}
	before, _ := readStateAt(s, "", c1, nil)
	if ir.Get(mustGet(t, before, "a"), "x") == nil {
		t.Errorf("the state at %d lost x: the rename reached back", c1)
	}

	stored := strings.Join(storedPatches(t, s), " | ")
	if strings.Contains(stored, "!field") {
		t.Errorf("the log holds the relative operation: %s", stored)
	}
	// A watcher folding the delta for c2 onto the state at c1 lands on the state at c2.
	ns, err := readPatchesInRange(s, "", c2, c2, nil)
	if err != nil || len(ns) != 1 {
		t.Fatalf("delta for %d: %v %v", c2, ns, err)
	}
	stepped, err := applyStoredPatch(before, ns[0].Patch)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if !stepped.DeepEqual(after) {
		t.Errorf("folding the delta gives %s, the read gives %s", mustEncode(t, stepped), mustEncode(t, after))
	}
}

func mustGet(t *testing.T, n *ir.Node, kp string) *ir.Node {
	t.Helper()
	v, err := n.GetKPath(kp)
	if err != nil || v == nil {
		t.Fatalf("no %s in %s", kp, mustEncode(t, n))
	}
	return v
}
