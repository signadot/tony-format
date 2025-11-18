package schema

import (
	"fmt"
	"sync"

	"github.com/signadot/tony-format/go-tony/ir"
)

// SchemaRegistry manages all known schemas
type SchemaRegistry struct {
	mu sync.RWMutex

	// Map of schema name -> Schema (within a context)
	// Key format: "contextURI:name" or just "name" for default context
	schemas map[string]*Schema

	// Map of schema URI -> Schema (cross-context, full URI)
	schemasByURI map[string]*Schema

	// Context registry for resolving contexts
	contexts *ContextRegistry
}

// NewSchemaRegistry creates a new schema registry
func NewSchemaRegistry(contextRegistry *ContextRegistry) *SchemaRegistry {
	return &SchemaRegistry{
		schemas:      make(map[string]*Schema),
		schemasByURI: make(map[string]*Schema),
		contexts:     contextRegistry,
	}
}

// RegisterSchema registers a schema
func (r *SchemaRegistry) RegisterSchema(schema *Schema) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if schema.Signature == nil || schema.Signature.Name == "" {
		return fmt.Errorf("schema must have a signature with a name")
	}

	if schema.Context == nil {
		return fmt.Errorf("schema must have a context")
	}

	// Register by schema name (context handling is separate)
	key := schema.Signature.Name
	if _, exists := r.schemas[key]; exists {
		return fmt.Errorf("schema %q already registered", schema.Signature.Name)
	}

	r.schemas[key] = schema

	// If schema has a full URI (from signature or elsewhere), register that too
	// TODO: Add URI field to Signature if needed

	return nil
}

// ResolveSchema resolves a schema by reference
func (r *SchemaRegistry) ResolveSchema(ref *SchemaReference) (*Schema, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// If reference has a URI, try that first
	if ref.URI != "" {
		if schema, exists := r.schemasByURI[ref.URI]; exists {
			return schema, nil
		}
		return nil, fmt.Errorf("schema with URI %q not found", ref.URI)
	}

	// Resolve by name
	if ref.Name == "" {
		return nil, fmt.Errorf("schema reference must have either URI or Name")
	}

	if schema, exists := r.schemas[ref.Name]; exists {
		return schema, nil
	}

	return nil, fmt.Errorf("schema %q not found", ref.Name)
}

// schemaKey creates a key for schema lookup
func (r *SchemaRegistry) schemaKey(contextURI, name string) string {
	return fmt.Sprintf("%s:%s", contextURI, name)
}

// GetSchema returns a schema by name
func (r *SchemaRegistry) GetSchema(name string) (*Schema, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schema, exists := r.schemas[name]
	return schema, exists
}

// AllSchemas returns all registered schemas
func (r *SchemaRegistry) AllSchemas() []*Schema {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Schema, 0, len(r.schemas))
	for _, schema := range r.schemas {
		result = append(result, schema)
	}
	return result
}

// ResolveDefinition resolves a FromReference to get the actual definition node from another schema
// Example: ResolveDefinition(&FromReference{SchemaName: "base-schema", DefName: "number"})
// returns the definition node for "number" from the "base-schema" schema
func (r *SchemaRegistry) ResolveDefinition(ref *FromReference) (*ir.Node, error) {
	if ref == nil {
		return nil, fmt.Errorf("from reference cannot be nil")
	}

	// Create a schema reference to resolve the schema
	schemaRef := &SchemaReference{
		Name: ref.SchemaName,
		Args: ref.SchemaArgs,
	}

	// Resolve the schema
	schema, err := r.ResolveSchema(schemaRef)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve schema %q: %w", ref.SchemaName, err)
	}

	// Look up the definition in the schema's Define map
	if schema.Define == nil {
		return nil, fmt.Errorf("schema %q has no definitions", ref.SchemaName)
	}

	defNode, exists := schema.Define[ref.DefName]
	if !exists {
		return nil, fmt.Errorf("definition %q not found in schema %q", ref.DefName, ref.SchemaName)
	}

	return defNode, nil
}
