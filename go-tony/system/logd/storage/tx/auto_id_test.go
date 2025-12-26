package tx

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

func TestInjectAutoIDs(t *testing.T) {
	tests := []struct {
		name          string
		schema        *api.Schema
		patchPath     string // path for the patch (default "")
		patchData     string
		commit        int64
		expectedCount int
		checkFields   map[string]bool // path -> expect non-empty id
	}{
		{
			name:          "nil schema",
			schema:        nil,
			patchData:     `users: [{ name: Alice }]`,
			commit:        1,
			expectedCount: 0,
		},
		{
			name:          "no auto-id fields in schema",
			schema:        &api.Schema{AutoIDFields: []api.AutoIDField{}},
			patchData:     `users: [{ name: Alice }]`,
			commit:        1,
			expectedCount: 0,
		},
		{
			name: "inject id into missing field",
			schema: &api.Schema{AutoIDFields: []api.AutoIDField{
				{Path: "users", Field: "id"},
			}},
			patchData:     `users: [{ name: Alice }]`,
			commit:        1,
			expectedCount: 1,
			checkFields:   map[string]bool{"users[0].id": true},
		},
		{
			name: "inject id into null field",
			schema: &api.Schema{AutoIDFields: []api.AutoIDField{
				{Path: "users", Field: "id"},
			}},
			patchData:     `users: [{ id: null, name: Alice }]`,
			commit:        1,
			expectedCount: 1,
			checkFields:   map[string]bool{"users[0].id": true},
		},
		{
			name: "preserve existing id",
			schema: &api.Schema{AutoIDFields: []api.AutoIDField{
				{Path: "users", Field: "id"},
			}},
			patchData:     `users: [{ id: existing-id, name: Alice }]`,
			commit:        1,
			expectedCount: 0,
		},
		{
			name: "multiple elements",
			schema: &api.Schema{AutoIDFields: []api.AutoIDField{
				{Path: "users", Field: "id"},
			}},
			patchData:     `users: [{ name: Alice }, { name: Bob }, { id: existing, name: Charlie }]`,
			commit:        1,
			expectedCount: 2, // Alice and Bob get IDs, Charlie already has one
		},
		{
			name: "nested auto-id",
			schema: &api.Schema{AutoIDFields: []api.AutoIDField{
				{Path: "orders.items", Field: "sku"},
			}},
			patchData:     `orders: { items: [{ qty: 1 }, { sku: existing, qty: 2 }] }`,
			commit:        1,
			expectedCount: 1, // Only first item needs ID
		},
		{
			name: "patch at path",
			schema: &api.Schema{AutoIDFields: []api.AutoIDField{
				{Path: "users", Field: "id"},
			}},
			patchPath:     "users",
			patchData:     `[{ name: Alice }]`,
			commit:        1,
			expectedCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := parse.Parse([]byte(tt.patchData))
			if err != nil {
				t.Fatalf("failed to parse patch data: %v", err)
			}

			data := []*PatcherData{{
				API: &api.Patch{
					PathData: api.PathData{
						Path: tt.patchPath,
						Data: node,
					},
				},
			}}

			count := InjectAutoIDs(tt.commit, tt.schema, data)

			if count != tt.expectedCount {
				t.Errorf("InjectAutoIDs returned %d, expected %d", count, tt.expectedCount)
			}

			// Check specific fields if requested
			for path, expectNonEmpty := range tt.checkFields {
				val := getNestedValue(node, path)
				if expectNonEmpty {
					if val == nil || val.Type != ir.StringType || val.String == "" {
						t.Errorf("expected non-empty string at path %q, got %v", path, val)
					}
				}
			}
		})
	}
}

func TestInjectAutoIDsMonotonicity(t *testing.T) {
	schema := &api.Schema{AutoIDFields: []api.AutoIDField{
		{Path: "items", Field: "id"},
	}}

	// Create patches across multiple commits
	var allIDs []string

	for commit := int64(1); commit <= 5; commit++ {
		patchData := `items: [{ name: a }, { name: b }]`
		node, err := parse.Parse([]byte(patchData))
		if err != nil {
			t.Fatalf("failed to parse: %v", err)
		}

		data := []*PatcherData{{
			API: &api.Patch{
				PathData: api.PathData{
					Path: "",
					Data: node,
				},
			},
		}}

		InjectAutoIDs(commit, schema, data)

		// Extract IDs
		items := ir.Get(node, "items")
		if items == nil || items.Type != ir.ArrayType {
			t.Fatalf("expected items array")
		}
		for _, item := range items.Values {
			idNode := ir.Get(item, "id")
			if idNode == nil || idNode.Type != ir.StringType {
				t.Fatal("expected id string")
			}
			allIDs = append(allIDs, idNode.String)
		}
	}

	// Verify all IDs are unique and monotonic
	seen := make(map[string]bool)
	var prev string
	for i, id := range allIDs {
		if seen[id] {
			t.Errorf("duplicate ID at index %d: %q", i, id)
		}
		seen[id] = true

		if prev != "" && id <= prev {
			t.Errorf("IDs not monotonic at index %d: %q <= %q", i, id, prev)
		}
		prev = id
	}
}

// getNestedValue gets a value from a node using a simple path like "users[0].id"
func getNestedValue(node *ir.Node, path string) *ir.Node {
	// Simple path parser for testing
	current := node
	start := 0

	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == '.' || path[i] == '[' {
			if i > start {
				key := path[start:i]
				current = ir.Get(current, key)
				if current == nil {
					return nil
				}
			}
			start = i + 1
		}

		if i < len(path) && path[i] == '[' {
			// Parse array index
			j := i + 1
			for j < len(path) && path[j] != ']' {
				j++
			}
			idxStr := path[i+1 : j]
			var idx int
			for _, c := range idxStr {
				idx = idx*10 + int(c-'0')
			}
			if current.Type != ir.ArrayType || idx >= len(current.Values) {
				return nil
			}
			current = current.Values[idx]
			i = j
			start = j + 1
			if start < len(path) && path[start] == '.' {
				start++
			}
		}
	}

	return current
}
