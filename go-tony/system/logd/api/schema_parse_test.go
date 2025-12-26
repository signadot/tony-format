package api

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
)

func TestParseSchemaFromNode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []AutoIDField
	}{
		{
			name:  "single auto-id field",
			input: `define: { users: { id: !logd-auto-id string, name: string } }`,
			expected: []AutoIDField{
				{Path: "users", Field: "id"},
			},
		},
		{
			name:  "nested auto-id field",
			input: `define: { orders: { items: { sku: !logd-auto-id string, qty: number } } }`,
			expected: []AutoIDField{
				{Path: "orders.items", Field: "sku"},
			},
		},
		{
			name:  "multiple auto-id fields",
			input: `define: { users: { id: !logd-auto-id string, name: string }, orders: { items: { sku: !logd-auto-id string, qty: number } } }`,
			expected: []AutoIDField{
				{Path: "users", Field: "id"},
				{Path: "orders.items", Field: "sku"},
			},
		},
		{
			name:     "no auto-id fields",
			input:    `define: { users: { id: string } }`,
			expected: nil,
		},
		{
			name:     "no define section",
			input:    `accept: { any: any }`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := parse.Parse([]byte(tt.input))
			if err != nil {
				t.Fatalf("failed to parse: %v", err)
			}

			schema := ParseSchemaFromNode(node)

			if tt.expected == nil {
				if schema != nil {
					t.Errorf("expected nil schema, got %+v", schema)
				}
				return
			}

			if schema == nil {
				t.Fatalf("expected schema, got nil")
			}

			if len(schema.AutoIDFields) != len(tt.expected) {
				t.Fatalf("expected %d fields, got %d", len(tt.expected), len(schema.AutoIDFields))
			}

			for i, exp := range tt.expected {
				got := schema.AutoIDFields[i]
				if got.Path != exp.Path || got.Field != exp.Field {
					t.Errorf("field %d: expected {%q, %q}, got {%q, %q}",
						i, exp.Path, exp.Field, got.Path, got.Field)
				}
			}
		})
	}
}
