package index

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// The index has no schema and needs none. A keyed array reaches it in the form the store
// keeps -- an object whose fields are the elements' names (element_identity.md) -- so an
// element is recorded as a field, and an array that is still an array is positional.
func TestIndexRecordsNamesAsFieldsAndPositionsAsPositions(t *testing.T) {
	tests := []struct {
		name           string
		patch          string
		expectedPaths  []string
		notExpectPaths []string
	}{
		{
			name: "a keyed array in its stored form",
			patch: `users:
  "(id=joe)": {id: joe, name: Joe}
  "(id=alice)": {id: alice, name: Alice}
`,
			expectedPaths:  []string{"", "users", kpath.ChildField("users", "(id=joe)"), kpath.ChildField("users", "(id=alice)")},
			notExpectPaths: []string{"users[0]", "users[1]", "users(joe)"},
		},
		{
			name: "a nested keyed array in its stored form",
			patch: `orders:
  items:
    "(sku=ABC)": {sku: ABC, qty: 2}
    "(sku=XYZ)": {sku: XYZ, qty: 1}
`,
			expectedPaths:  []string{"", "orders", "orders.items", kpath.ChildField("orders.items", "(sku=ABC)"), kpath.ChildField("orders.items", "(sku=XYZ)") + ".qty"},
			notExpectPaths: []string{"orders.items[0]", "orders.items(ABC)"},
		},
		{
			name: "an array is positional",
			patch: `users:
- id: joe
  name: Joe
`,
			expectedPaths:  []string{"", "users", "users[0]", "users[0].name"},
			notExpectPaths: []string{"users(joe)", kpath.ChildField("users", "(id=joe)")},
		},
		{
			// A !key tag on a patch declares nothing to the index; the schema declares an
			// identity, and lowering has already turned such an array into its names by
			// the time it is indexed. One that arrives tagged is indexed as it is.
			name:           "a !key tag is not an identity",
			patch:          `{items: !key(sku) [{sku: WIDGET, qty: 1}]}`,
			expectedPaths:  []string{"items", "items[0]"},
			notExpectPaths: []string{"items(WIDGET)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := NewIndex("")
			node, err := parse.Parse([]byte(tt.patch))
			if err != nil {
				t.Fatalf("failed to parse patch: %v", err)
			}
			lastCommit := int64(0)
			entry := &dlog.Entry{Commit: 1, LastCommit: &lastCommit, Patch: node}
			IndexPatch(idx, entry, "A", 0, 1, 0, node, nil)

			indexed := map[string]bool{}
			for _, seg := range idx.AllSegments() {
				indexed[seg.KindedPath] = true
			}
			for _, path := range tt.expectedPaths {
				if !indexed[path] {
					t.Errorf("expected path %q not found in index; have %v", path, indexed)
					continue
				}
				// The read side extracts the patch at the path it recorded, so a path
				// that does not lead back to a node would drop the patch.
				if at, err := node.GetKPath(path); err != nil || at == nil {
					t.Errorf("indexed path %q does not reach into the patch: %v", path, err)
				}
			}
			for _, path := range tt.notExpectPaths {
				if indexed[path] {
					t.Errorf("unexpected path %q found in index", path)
				}
			}
		})
	}
}
