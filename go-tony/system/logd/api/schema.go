package api

import (
	"fmt"
	"slices"
	"strings"

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

// Identity answers the fields that identify an element of the array at kpath: every
// !logd-key declared on it, as one tuple, or the one !logd-auto-id field. Sorted, so two
// callers spell the same identity the same way, and nil for an array the schema does not
// key.
//
// Both declarations mean "keyed": an explicit !logd-key, and an !logd-auto-id, which is
// keying plus generation. A keyed array has ONE identity; several !logd-key fields on one
// array are the parts of it, not alternatives (element_identity.md), and Validate refuses
// an array that declares both kinds.
func (s *Schema) Identity(kpath string) []string {
	if s == nil {
		return nil
	}
	var fields []string
	for _, f := range s.KeyFields {
		if f.Path == kpath && !slices.Contains(fields, f.Field) {
			fields = append(fields, f.Field)
		}
	}
	if len(fields) > 0 {
		slices.Sort(fields)
		return fields
	}
	for _, f := range s.AutoIDFields {
		if f.Path == kpath {
			return []string{f.Field}
		}
	}
	return nil
}

// Keyed reports whether the array at kpath has an identity.
func (s *Schema) Keyed(kpath string) bool { return len(s.Identity(kpath)) > 0 }

// KeyedPaths answers every array path the schema gives an identity to.
func (s *Schema) KeyedPaths() []string {
	if s == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, f := range s.KeyFields {
		if !seen[f.Path] {
			seen[f.Path] = true
			out = append(out, f.Path)
		}
	}
	for _, f := range s.AutoIDFields {
		if !seen[f.Path] {
			seen[f.Path] = true
			out = append(out, f.Path)
		}
	}
	slices.Sort(out)
	return out
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
		// The name of an element binds field to value as (f=v), split at the first '=',
		// so a field whose own name holds one would be read as a different binding.
		if strings.ContainsAny(f.Field, "=<>") {
			return fmt.Errorf("%q is declared keyed by %q, which cannot name an element: an "+
				"identity field is written into the element's name as (field=value), and "+
				"'=', '<' and '>' are the name's own punctuation", f.Path, f.Field)
		}
		// Several !logd-key fields on one array are one identity, the tuple of them.
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
