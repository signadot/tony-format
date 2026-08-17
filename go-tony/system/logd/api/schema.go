package api

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
)

// AutoIDField describes a field that is auto-generated and serves as the key
// for a keyed array. Derived from !logd-auto-id tags in Tony schema.
type AutoIDField struct {
	Path  string // kpath to the parent array (e.g., "users", "orders.items")
	Field string // field name within array elements (e.g., "id")
}

// KeyField declares that an array is merged and indexed by identity, on a key the CLIENT
// supplies. Derived from !logd-key.
//
// Auto-id is a kind of keying that additionally generates the value; this is the other
// kind, and until it existed "items is keyed by name" was unsayable in schema -- a schema
// could only call an array keyed as a side effect of calling its key auto-generated.
type KeyField struct {
	Path  string // kpath to the array (e.g. "items", "orders.items")
	Field string // field within an element that identifies it (e.g. "name", "meta.name")
}

// Schema defines data model constraints for logd.
// Derived from Tony schema by parsing !logd-auto-id and !logd-key tags.
type Schema struct {
	// AutoIDFields lists fields that are auto-generated.
	// Each entry implies the parent is a keyed array indexed by that field.
	AutoIDFields []AutoIDField

	// KeyFields lists arrays keyed on a client-supplied field.
	KeyFields []KeyField
}

// LookupKeyField returns the key field for a given kpath, or empty if not keyed.
//
// Both declarations mean "keyed": an explicit !logd-key, and an !logd-auto-id, which is
// keying plus generation. An explicit key wins if a schema somehow declares both -- though
// Validate rejects that, so it should not arise.
func (s *Schema) LookupKeyField(kpath string) string {
	if s == nil {
		return ""
	}
	for _, f := range s.KeyFields {
		if f.Path == kpath {
			return f.Field
		}
	}
	for _, f := range s.AutoIDFields {
		if f.Path == kpath {
			return f.Field
		}
	}
	return ""
}

// Validate reports a schema that cannot mean what it says. It is checked where a schema
// is PROPOSED (StartMigration), so a store never adopts one whose keying is ambiguous --
// key derivation decides what a stored delta records, and a delta cannot be un-recorded.
func (s *Schema) Validate() error {
	if s == nil {
		return nil
	}
	keyed := map[string]string{}
	for _, f := range s.KeyFields {
		if f.Field == "" {
			return fmt.Errorf("!logd-key at %q names no field: the index turns each element "+
				"into a path segment from a scalar field, so keying by the element itself "+
				"cannot be represented", f.Path)
		}
		// The key field's name is written into a !key tag, and a tag argument has
		// no quoting: a name holding a comma or an unbalanced parenthesis would be
		// written as a tag which says something else.  ir.TagCompose refuses such a
		// name, so the schema which declares it is refused here, where the error can
		// name it (b6ad0qw0h12krhk5gdn0).
		if !ir.TagArgOK(f.Field) {
			return fmt.Errorf("%q is declared keyed by %q, which cannot be written in a !key tag: "+
				"a tag argument has no quoting, so a comma or an unbalanced parenthesis in the "+
				"name would name something else", f.Path, f.Field)
		}
		if prev, dup := keyed[f.Path]; dup && prev != f.Field {
			return fmt.Errorf("%q is declared keyed by both %q and %q", f.Path, prev, f.Field)
		}
		keyed[f.Path] = f.Field
	}
	for _, f := range s.AutoIDFields {
		if prev, dup := keyed[f.Path]; dup && prev != f.Field {
			return fmt.Errorf("%q is declared keyed by %q and auto-id on %q; one array has "+
				"one identity", f.Path, prev, f.Field)
		}
		keyed[f.Path] = f.Field
	}
	return nil
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
