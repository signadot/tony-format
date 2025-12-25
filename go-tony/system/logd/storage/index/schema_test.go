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
			schema: &api.Schema{KeyedArrays: map[string]string{"users": "id"}},
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
			schema: &api.Schema{KeyedArrays: map[string]string{"orders.items": "sku"}},
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

			err = IndexPatch(idx, entry, "A", 0, 1, node, tt.schema, nil)
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

	schema := &api.Schema{KeyedArrays: map[string]string{"users": "id"}}
	lastCommit := int64(0)
	entry := &dlog.Entry{
		Commit:     1,
		LastCommit: &lastCommit,
		Patch:      node,
	}

	err = IndexPatch(idx, entry, "A", 0, 1, node, schema, nil)
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
