package index

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

func TestIndexPatchWithSchema(t *testing.T) {
	tests := []struct {
		name           string
		schema         *api.Schema
		patch          string
		expectedPaths  []string
		notExpectPaths []string
	}{
		{
			name:   "schema keyed array",
			schema: &api.Schema{AutoIDFields: []api.AutoIDField{{Path: "users", Field: "id"}}},
			patch: `users:
- id: joe
  name: Joe
- id: alice
  name: Alice
`,
			expectedPaths:  []string{"", "users", "users(joe)", "users(alice)"},
			notExpectPaths: []string{"users[0]", "users[1]"},
		},
		{
			name:   "schema nested keyed array",
			schema: &api.Schema{AutoIDFields: []api.AutoIDField{{Path: "orders.items", Field: "sku"}}},
			patch: `orders:
  items:
  - sku: ABC
    qty: 2
  - sku: XYZ
    qty: 1
`,
			expectedPaths:  []string{"", "orders", "orders.items", "orders.items(ABC)", "orders.items(XYZ)"},
			notExpectPaths: []string{"orders.items[0]", "orders.items[1]"},
		},
		{
			name:   "no schema falls back to positional",
			schema: nil,
			patch: `users:
- id: joe
  name: Joe
`,
			expectedPaths:  []string{"", "users", "users[0]"},
			notExpectPaths: []string{"users(joe)"},
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
			entry := &dlog.Entry{
				Commit:     1,
				LastCommit: &lastCommit,
				Patch:      node,
			}

			err = IndexPatch(idx, entry, "A", 0, 1, 0, node, tt.schema, nil)
			if err != nil {
				t.Fatalf("IndexPatch failed: %v", err)
			}

			// Check expected paths exist
			for _, path := range tt.expectedPaths {
				segs := idx.LookupRange(path, nil, nil, nil)
				found := false
				for _, seg := range segs {
					if seg.KindedPath == path {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected path %q not found in index", path)
				}
			}

			// Check paths that should not exist
			for _, path := range tt.notExpectPaths {
				segs := idx.LookupRange(path, nil, nil, nil)
				for _, seg := range segs {
					if seg.KindedPath == path {
						t.Errorf("unexpected path %q found in index", path)
					}
				}
			}
		})
	}
}

func TestIndexPatchKeyedArrayBugFix(t *testing.T) {
	// This test verifies the bug fix where ir.Get(n, key) was incorrectly
	// using 'n' (the array) instead of 'v' (the element object)
	idx := NewIndex("")

	patch := `users:
- id: joe
  email: joe@example.com
- id: alice
  email: alice@example.com
`

	node, err := parse.Parse([]byte(patch))
	if err != nil {
		t.Fatalf("failed to parse patch: %v", err)
	}

	schema := &api.Schema{AutoIDFields: []api.AutoIDField{{Path: "users", Field: "id"}}}
	lastCommit := int64(0)
	entry := &dlog.Entry{
		Commit:     1,
		LastCommit: &lastCommit,
		Patch:      node,
	}

	err = IndexPatch(idx, entry, "A", 0, 1, 0, node, schema, nil)
	if err != nil {
		t.Fatalf("IndexPatch failed: %v", err)
	}

	// Verify keyed paths were created with actual key values
	expectedPaths := []string{"users(joe)", "users(alice)"}
	for _, path := range expectedPaths {
		segs := idx.LookupRange(path, nil, nil, nil)
		found := false
		for _, seg := range segs {
			if seg.KindedPath == path {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected keyed path %q not found - bug may not be fixed", path)
		}
	}

	// Verify positional paths were NOT created
	badPaths := []string{"users[0]", "users[1]"}
	for _, path := range badPaths {
		segs := idx.LookupRange(path, nil, nil, nil)
		for _, seg := range segs {
			if seg.KindedPath == path {
				t.Errorf("positional path %q should not exist when schema defines keyed array", path)
			}
		}
	}
}

// TestIndexPatchKeyTag covers the other source of a key field: the !key(...)
// tag the patch itself carries.  A path recorded from it has to be a path a
// reader can follow back through the same patch, which is the check at the end.
func TestIndexPatchKeyTag(t *testing.T) {
	tests := []struct {
		name           string
		schema         *api.Schema
		patch          string
		expectedPaths  []string
		notExpectPaths []string
	}{
		{
			name:           "keyed by a field",
			patch:          `{items: !key(sku) [{sku: WIDGET, qty: 1}, {sku: GADGET, qty: 2}]}`,
			expectedPaths:  []string{"items(WIDGET)", "items(GADGET)", "items(WIDGET).qty"},
			notExpectPaths: []string{"items[0]", "items[1]"},
		},
		{
			name:           "keyed by a nested field",
			patch:          `{items: !key(meta.name) [{meta: {name: joe}, qty: 1}]}`,
			expectedPaths:  []string{"items(joe)", "items(joe).qty"},
			notExpectPaths: []string{"items[0]"},
		},
		{
			name:           "a bare !key keys the elements by themselves",
			patch:          `{items: !key [joe, bob]}`,
			expectedPaths:  []string{"items(joe)", "items(bob)"},
			notExpectPaths: []string{"items[0]", "items[1]"},
		},
		{
			// a key which would read as a number is quoted, so the segment says
			// key rather than index
			name:           "keyed by a number",
			patch:          `{items: !key(id) [{id: 7, qty: 1}]}`,
			expectedPaths:  []string{`items("7")`, `items("7").qty`},
			notExpectPaths: []string{"items[0]", "items(7)"},
		},
		{
			// the schema is asked first, so it decides when both say something
			name:           "schema wins over the tag",
			schema:         &api.Schema{AutoIDFields: []api.AutoIDField{{Path: "items", Field: "id"}}},
			patch:          `{items: !key(sku) [{id: 7, sku: WIDGET}]}`,
			expectedPaths:  []string{`items("7")`},
			notExpectPaths: []string{"items(WIDGET)", "items[0]"},
		},
		{
			name:           "an untagged list stays positional",
			patch:          `{items: [{sku: WIDGET, qty: 1}]}`,
			expectedPaths:  []string{"items[0]"},
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
			if err := IndexPatch(idx, entry, "A", 0, 1, 0, node, tt.schema, nil); err != nil {
				t.Fatalf("IndexPatch failed: %v", err)
			}
			indexed := map[string]bool{}
			for _, seg := range idx.AllSegments() {
				indexed[seg.KindedPath] = true
			}
			for _, path := range tt.expectedPaths {
				if !indexed[path] {
					t.Errorf("expected path %q not found in index", path)
					continue
				}
				// the read side extracts the patch at the path it recorded, so a
				// path which does not lead back to a node would drop the patch
				if tt.schema != nil {
					continue // a schema key is not written down in the patch
				}
				at, err := node.GetKPath(path)
				if err != nil || at == nil {
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
