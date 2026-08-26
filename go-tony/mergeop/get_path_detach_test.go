package mergeop

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// The answer is a COPY, detached: a node belongs to one tree, and the patch side
// installs what it is handed, so the document's own node would be spliced out of
// the document.  Detached is also where the walk stops -- navigating down is what
// the path already did, so !get-path(root) inside such a value means that value.
func TestGetPathAnswersADetachedCopy(t *testing.T) {
	doc := ir.FromKeyVals([]ir.KeyVal{
		{Key: ir.FromString("a"), Val: ir.FromKeyVals([]ir.KeyVal{
			{Key: ir.FromString("x"), Val: ir.FromInt(1)},
		})},
	})

	t.Run("get-path", func(t *testing.T) {
		op, err := GetPath().Instance(ir.FromString("a"), nil)
		if err != nil {
			t.Fatalf("instance: %v", err)
		}
		got, err := op.Patch(doc, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("patch: %v", err)
		}
		if got == doc.Values[0] {
			t.Fatal("the answer IS the document's node, which the patch side would splice out of it")
		}
		if got.Parent != nil {
			t.Errorf("the answer has a parent, so a walk up leaves what was asked for")
		}
		if got.ParentField != "" || got.ParentIndex != 0 {
			t.Errorf("the answer still claims a place: field %q index %d", got.ParentField, got.ParentIndex)
		}
		if doc.Values[0].Parent != doc {
			t.Errorf("the document's own node was re-parented out of it")
		}
	})

	t.Run("get-paths", func(t *testing.T) {
		op, err := GetPaths().Instance(ir.FromString("a"), nil)
		if err != nil {
			t.Fatalf("instance: %v", err)
		}
		got, err := op.Patch(doc, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("patch: %v", err)
		}
		if got.Parent != nil {
			t.Errorf("the list has a parent")
		}
		if len(got.Values) != 1 {
			t.Fatalf("the list holds %d values, want 1", len(got.Values))
		}
		if got.Values[0] == doc.Values[0] {
			t.Error("the list holds the document's own node, which FromSlice re-parents")
		}
		if got.Values[0].Parent != got {
			t.Error("the list's value does not point back at the list")
		}
		if doc.Values[0].Parent != doc {
			t.Errorf("the document's own node was re-parented out of it")
		}
	})
}
