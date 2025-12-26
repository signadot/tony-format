package api

// AutoIDField describes a field that is auto-generated and serves as the key
// for a keyed array. Derived from !logd-auto-id tags in Tony schema.
type AutoIDField struct {
	Path  string // kpath to the parent array (e.g., "users", "orders.items")
	Field string // field name within array elements (e.g., "id")
}

// Schema defines data model constraints for logd.
// Derived from Tony schema by parsing !logd-auto-id tags.
type Schema struct {
	// AutoIDFields lists fields that are auto-generated.
	// Each entry implies the parent is a keyed array indexed by that field.
	AutoIDFields []AutoIDField
}

// LookupKeyField returns the key field for a given kpath, or empty if not keyed.
// For example, if AutoIDFields contains {Path: "users", Field: "id"},
// then LookupKeyField("users") returns "id".
func (s *Schema) LookupKeyField(kpath string) string {
	if s == nil {
		return ""
	}
	for _, f := range s.AutoIDFields {
		if f.Path == kpath {
			return f.Field
		}
	}
	return ""
}

// AutoID returns the auto-id config for a kpath, or nil if not auto-id.
func (s *Schema) AutoID(kpath string) *AutoIDField {
	if s == nil {
		return nil
	}
	for i := range s.AutoIDFields {
		if s.AutoIDFields[i].Path == kpath {
			return &s.AutoIDFields[i]
		}
	}
	return nil
}

// SchemaResolver provides schema for a given scope.
// This allows different scopes to have different schemas.
type SchemaResolver interface {
	// GetSchema returns schema for the given scope.
	// scopeID nil = baseline schema
	GetSchema(scopeID *string) *Schema
}

// StaticSchemaResolver returns the same schema for all scopes.
type StaticSchemaResolver struct {
	Schema *Schema
}

// GetSchema returns the static schema regardless of scope.
func (r *StaticSchemaResolver) GetSchema(scopeID *string) *Schema {
	return r.Schema
}
