package gomap

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/schema"
)

// resolveSchema resolves a schema reference from struct tag information.
// It handles:
//   - Schema names (short names via context registry)
//   - Schema URIs (fully qualified)
//   - Returns nil if no schema found (fall back to reflection)
func (m *Mapper) resolveSchema(schemaName string) (*schema.Schema, error) {
	if m.schemaRegistry == nil {
		// No schema registry available, return nil to fall back to reflection
		return nil, nil
	}

	// Check if schemaName is a URI (contains "://" or starts with "urn:")
	isURI := false
	if len(schemaName) > 0 {
		// Simple heuristic: URIs typically contain "://" or start with "urn:"
		for i := 0; i < len(schemaName)-2; i++ {
			if schemaName[i:i+3] == "://" {
				isURI = true
				break
			}
		}
		if !isURI && len(schemaName) >= 4 && schemaName[:4] == "urn:" {
			isURI = true
		}
	}

	if isURI {
		// Fully qualified URI - resolve directly
		ref := &schema.SchemaReference{
			URI: schemaName,
		}
		return m.schemaRegistry.ResolveSchema(ref)
	}

	// Short name - resolve via context registry
	// First, try to get schema by name (may be in default context)
	if s, ok := m.schemaRegistry.GetSchema(schemaName); ok {
		return s, nil
	}

	// If context registry is available, try to resolve via context
	if m.contextRegistry != nil {
		// Try to resolve as a short name through contexts
		ref := &schema.SchemaReference{
			Name: schemaName,
		}
		return m.schemaRegistry.ResolveSchema(ref)
	}

	// Schema not found - return nil to fall back to reflection
	return nil, nil
}

// resolveDefinition resolves a definition reference within a schema.
// It handles:
//   - References within the same schema (e.g., ".[name]")
//   - Cross-schema references (e.g., "!from schema-name .name")
//   - Returns nil if not found (fall back to reflection)
func (m *Mapper) resolveDefinition(s *schema.Schema, defName string) (*ir.Node, error) {
	if s == nil {
		return nil, fmt.Errorf("schema cannot be nil")
	}

	if defName == "" {
		return nil, fmt.Errorf("definition name cannot be empty")
	}

	// First, try to resolve within the same schema
	if s.Define != nil {
		if defNode, exists := s.Define[defName]; exists {
			return defNode, nil
		}
	}

	// If schema registry is available, try cross-schema resolution
	// We need the schema name to resolve cross-schema references
	if m.schemaRegistry != nil && s.Signature != nil && s.Signature.Name != "" {
		ref := &schema.FromReference{
			SchemaName: s.Signature.Name,
			DefName:    defName,
		}
		return m.schemaRegistry.ResolveDefinition(ref)
	}

	// Definition not found
	return nil, fmt.Errorf("definition %q not found in schema", defName)
}
