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
				AutoIDFields: []AutoIDField{
					{Path: "users", Field: "id"},
				},
			},
			kpath:    "users",
			expected: "id",
		},
		{
			name: "nested path",
			schema: &Schema{
				AutoIDFields: []AutoIDField{
					{Path: "orders.items", Field: "sku"},
				},
			},
			kpath:    "orders.items",
			expected: "sku",
		},
		{
			name: "no match",
			schema: &Schema{
				AutoIDFields: []AutoIDField{
					{Path: "users", Field: "id"},
				},
			},
			kpath:    "posts",
			expected: "",
		},
		{
			name: "partial path no match",
			schema: &Schema{
				AutoIDFields: []AutoIDField{
					{Path: "users", Field: "id"},
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

func TestSchema_AutoID(t *testing.T) {
	schema := &Schema{
		AutoIDFields: []AutoIDField{
			{Path: "users", Field: "id"},
			{Path: "orders.items", Field: "sku"},
		},
	}

	// Test match
	aid := schema.AutoID("users")
	if aid == nil {
		t.Fatal("AutoID(users) should return non-nil")
	}
	if aid.Field != "id" {
		t.Errorf("AutoID(users).Field = %q, want %q", aid.Field, "id")
	}

	// Test no match
	aid = schema.AutoID("posts")
	if aid != nil {
		t.Errorf("AutoID(posts) should return nil, got %+v", aid)
	}

	// Test nil schema
	var nilSchema *Schema
	aid = nilSchema.AutoID("users")
	if aid != nil {
		t.Errorf("nil schema AutoID should return nil")
	}
}

func TestStaticSchemaResolver(t *testing.T) {
	schema := &Schema{
		AutoIDFields: []AutoIDField{
			{Path: "users", Field: "id"},
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
