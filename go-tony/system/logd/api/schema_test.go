package api

import "testing"

func TestSchema_LookupKeyField(t *testing.T) {
	tests := []struct {
		name     string
		schema   *Schema
		kpath    string
		expected string
	}{
		{
			name:     "nil schema",
			schema:   nil,
			kpath:    "users",
			expected: "",
		},
		{
			name:     "empty schema",
			schema:   &Schema{},
			kpath:    "users",
			expected: "",
		},
		{
			name: "exact match",
			schema: &Schema{
				KeyedArrays: map[string]string{
					"users": "id",
				},
			},
			kpath:    "users",
			expected: "id",
		},
		{
			name: "nested path",
			schema: &Schema{
				KeyedArrays: map[string]string{
					"orders.items": "sku",
				},
			},
			kpath:    "orders.items",
			expected: "sku",
		},
		{
			name: "no match",
			schema: &Schema{
				KeyedArrays: map[string]string{
					"users": "id",
				},
			},
			kpath:    "posts",
			expected: "",
		},
		{
			name: "partial path no match",
			schema: &Schema{
				KeyedArrays: map[string]string{
					"users": "id",
				},
			},
			kpath:    "users.items",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.schema.LookupKeyField(tt.kpath)
			if result != tt.expected {
				t.Errorf("LookupKeyField(%q) = %q, want %q", tt.kpath, result, tt.expected)
			}
		})
	}
}

func TestStaticSchemaResolver(t *testing.T) {
	schema := &Schema{
		KeyedArrays: map[string]string{
			"users": "id",
		},
	}
	resolver := &StaticSchemaResolver{Schema: schema}

	// Test nil scope (baseline)
	s := resolver.GetSchema(nil)
	if s != schema {
		t.Error("GetSchema(nil) should return the schema")
	}

	// Test with scope
	scopeID := "sandbox-123"
	s = resolver.GetSchema(&scopeID)
	if s != schema {
		t.Error("GetSchema with scope should return same schema")
	}
}
