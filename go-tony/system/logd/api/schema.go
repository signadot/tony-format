package api

// Schema defines data model constraints for logd.
// Designed to be extensible and scope-aware.
type Schema struct {
	// KeyedArrays maps kpath prefixes to key field names.
	// For example: {"users": "id", "orders.items": "sku"}
	// This tells logd that the "users" array should be indexed by the "id" field
	// of each element, using keyed array syntax like users(joe).
	KeyedArrays map[string]string `tony:"field=keyedArrays"`
}

// LookupKeyField returns the key field for a given kpath, or empty if not keyed.
// For example, if KeyedArrays is {"users": "id"}, then LookupKeyField("users")
// returns "id".
func (s *Schema) LookupKeyField(kpath string) string {
	if s == nil || s.KeyedArrays == nil {
		return ""
	}
	if keyField, ok := s.KeyedArrays[kpath]; ok {
		return keyField
	}
	return ""
}

// SchemaResolver provides schema for a given scope.
// This allows different scopes to have different schemas.
type SchemaResolver interface {
	// GetSchema returns schema for the given scope.
	// scopeID nil = baseline schema
	GetSchema(scopeID *string) *Schema
}

// StaticSchemaResolver returns the same schema for all scopes.
// This is the minimal implementation for initial !key support.
type StaticSchemaResolver struct {
	Schema *Schema
}

// GetSchema returns the static schema regardless of scope.
func (r *StaticSchemaResolver) GetSchema(scopeID *string) *Schema {
	return r.Schema
}
