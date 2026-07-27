package storage

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// Minimal repros for what TestReadEquivalence_SnapshotVsReference finds, one per
// failure class. These assert the CORRECT behaviour, so they fail until the read path
// is fixed; the harness finds them from seeds, these name them.
//
// All three share a cause worth stating once: the streaming processor only applies a
// patch when the base event stream passes through that patch's root path. Whatever the
// base does not contain, or resolves away, the patch cannot reach. The empty-base
// branch of ApplyPatches sidesteps this entirely by folding every entry with tony.Patch,
// which is why reads below the root — which never found a snapshot, so always had an
// empty base — have been correct while the snapshot path has not.

// Class 1: a write to a subtree the snapshot does not contain is dropped, and the next
// snapshot bakes in its absence. This is data loss, not staleness.
func TestSnapshotRead_NewSubtreeSurvives(t *testing.T) {
	s := openTestStorage(t)
	applyOp(t, s, genOp{path: "a.b", src: `{k1: 0}`})
	if err := s.SwitchDLog(); err != nil {
		t.Fatal(err)
	}
	commit, err := applyOp(t, s, genOp{path: "d.e", src: `{k1: 1}`})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.ReadStateAt("", commit, nil)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if nodeAt(got, "d", "e", "k1") == nil {
		t.Errorf("d.e.k1 written after the snapshot is missing from the read: %s", nodeText(got))
	}

	// And again after the next snapshot, which is built from the previous snapshot plus
	// the same dropped patch.
	if err := s.SwitchDLog(); err != nil {
		t.Fatal(err)
	}
	commit, err = applyOp(t, s, genOp{path: "a.b", src: `{k2: 2}`})
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.ReadStateAt("", commit, nil)
	if err != nil {
		t.Fatalf("read after second snapshot: %v", err)
	}
	if nodeAt(got, "d", "e", "k1") == nil {
		t.Errorf("d.e.k1 is gone from the snapshot chain for good: %s", nodeText(got))
	}
}

// Class 2: an ancestor-rooted write in the same replay range as descendant-rooted
// writes erases them. filterDominatedPaths drops the dominated patch nodes instead of
// folding them into the dominating path in commit order.
func TestSnapshotRead_AncestorWriteKeepsDescendantWrites(t *testing.T) {
	s := openTestStorage(t)
	applyOp(t, s, genOp{path: "a.b", src: `{hot: 1}`})
	if err := s.SwitchDLog(); err != nil {
		t.Fatal(err)
	}
	applyOp(t, s, genOp{path: "a.b", src: `{hot: 2}`})
	commit, err := applyOp(t, s, genOp{path: "a", src: `{b: {warm: 9}}`})
	if err != nil {
		t.Fatal(err)
	}

	// Read at the root: on the current tree only a root read consults a snapshot at
	// all, so a repro that reads at "a.b" passes for the wrong reason.
	got, err := s.ReadStateAt("", commit, nil)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	hot := nodeAt(got, "a", "b", "hot")
	if hot == nil || hot.Int64 == nil || *hot.Int64 != 2 {
		t.Errorf("a.b.hot = %s, want 2: the ancestor write at a reverted it to the snapshot value: %s",
			nodeText(hot), nodeText(got))
	}
	if nodeAt(got, "a", "b", "warm") == nil {
		t.Errorf("a.b.warm missing: %s", nodeText(got))
	}
}

// Class 3: a delete resolves a subtree to nil, and emitNode has no null case, so both
// the read and the NEXT SNAPSHOT fail. A failing SwitchDLog means snapshots stop
// entirely from then on.
func TestSnapshotRead_AncestorDelete(t *testing.T) {
	s := openTestStorage(t)
	applyOp(t, s, genOp{path: "a.b", src: `{k1: 0}`})
	applyOp(t, s, genOp{path: "d.e", src: `{k1: 1}`})
	if err := s.SwitchDLog(); err != nil {
		t.Fatal(err)
	}
	commit, err := applyOp(t, s, genOp{path: "a", src: `!delete`})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.ReadStateAt("", commit, nil)
	if err != nil {
		t.Errorf("read after an ancestor delete: %v", err)
	} else {
		if nodeAt(got, "a") != nil {
			t.Errorf("a survived its delete: %s", nodeText(got))
		}
		if nodeAt(got, "d", "e", "k1") == nil {
			t.Errorf("unrelated d.e lost: %s", nodeText(got))
		}
	}

	if err := s.SwitchDLog(); err != nil {
		t.Errorf("snapshot creation after a delete: %v", err)
	}
}

// nodeAt navigates n through the named object fields, returning nil if any is absent.
func nodeAt(n *ir.Node, fields ...string) *ir.Node {
	for _, field := range fields {
		if n == nil || n.Type != ir.ObjectType {
			return nil
		}
		var next *ir.Node
		for i, f := range n.Fields {
			if f.String == field {
				next = n.Values[i]
				break
			}
		}
		n = next
	}
	return n
}
