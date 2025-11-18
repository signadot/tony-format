package schema

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
)

// ResolveDefinitionName resolves a definition name within a schema's Define map.
//
// This function takes just the definition name (e.g., "number", "int"), not the
// full .[name] syntax that appears in Tony schema files. When processing schema
// definitions that contain .[name] references, extract the name first, then call
// this function.
//
// The .[name] syntax in Tony schemas works with expr-lang eval, where the schema's
// Define map acts as the environment. When a schema definition contains ".[number]",
// it evaluates to the definition node stored in schema.Define["number"].
//
// Example usage:
//   // In a Tony schema file:
//   define:
//     number: !type 1
//     int: !and
//       - .[number]    # This references the "number" definition above
//       - int: !not null
//
//   // When processing the "int" definition, extract the name from ".[number]":
//   refTag := ".[number]"  // As it appears in the schema
//   name := eval.GetRaw(refTag)  // Extracts "number" from ".[number]"
//   defNode, err := ResolveDefinitionName(schema, name)  // Looks up "number"
//
// Note: This function only resolves definitions within the same schema.
// For cross-schema references (using !from), use SchemaRegistry.ResolveDefinition.
func ResolveDefinitionName(schema *Schema, name string) (*ir.Node, error) {
	if schema == nil {
		return nil, fmt.Errorf("schema cannot be nil")
	}

	if name == "" {
		return nil, fmt.Errorf("definition name cannot be empty")
	}

	if schema.Define == nil {
		return nil, fmt.Errorf("schema has no definitions")
	}

	defNode, exists := schema.Define[name]
	if !exists {
		return nil, fmt.Errorf("definition %q not found in schema", name)
	}

	return defNode, nil
}
