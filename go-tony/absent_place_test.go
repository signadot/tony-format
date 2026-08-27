package tony

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// A patch field the document does not have is applied against a placeholder
// standing for the document's absence there. The placeholder used to be a bare
// ir.Null(), fabricated with no parent -- a node in no tree, and indistinguishable
// from a document root, since both answer nil to Parent.
//
// An operator which asks what document it is in got the placeholder and could not
// tell: !get-path(root) writing into a field the document did not have anchored at
// the placeholder rather than at the document, which errored, and !list-path(root)
// answered the EMPTY LIST -- a wrong answer, silently.
//
// It carries its place now. Nothing points down at it, so the document is
// unchanged and no second tree exists; what the patch means there is still what it
// means on its own, since the placeholder carries a place and no value.
func TestAbsentPlaceholderKnowsItsPlace(t *testing.T) {
	root := ir.FromKeyVals([]ir.KeyVal{
		{Key: ir.FromString("a"), Val: ir.FromKeyVals([]ir.KeyVal{
			{Key: ir.FromString("b"), Val: ir.FromInt(1)},
		})},
	})
	inner := root.Values[0]

	absent := absentAt(inner, "gone", 3)
	if absent.Type != ir.NullType {
		t.Errorf("the placeholder is a %s, and absence is null here", absent.Type)
	}
	if absent.Parent != inner {
		t.Errorf("the placeholder does not stand at the node it is absent from")
	}
	if absent.ParentField != "gone" || absent.ParentIndex != 3 {
		t.Errorf("the placeholder stands at field %q index %d, want %q and 3",
			absent.ParentField, absent.ParentIndex, "gone")
	}
	if absent.Root() != root {
		t.Errorf("the placeholder's root is not the document, so an operator asking " +
			"which document it is in cannot be told")
	}
	// and the document does not gain a child by being asked about one it lacks
	if len(inner.Values) != 1 || len(inner.Fields) != 1 {
		t.Errorf("the placeholder was spliced into the document: %d fields, %d values",
			len(inner.Fields), len(inner.Values))
	}
}
