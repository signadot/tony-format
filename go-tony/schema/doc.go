// Package schema provides support for Tony Schema, a schema system for describing
// and validating Tony format documents.
//
// # Overview
//
// Tony Schema is similar to JSON Schema but designed to be simpler, more lightweight,
// and more readable. Schemas describe constraints and information about documents
// in the Tony format, enabling:
//
//   - Precise modeling of data structures
//   - Document validation
//   - Documentation and communication between stakeholders
//   - Automation and code generation
//
// # Core Concepts
//
// ## Schema Documents
//
// A schema document consists of:
//
//   - Context: Execution context (match, patch, eval, etc.) that defines which
//     tags are available
//   - Signature: How the schema can be referenced (name and optional parameters)
//   - Define: Value definitions (like JSON Schema $defs)
//   - Accept: What documents this schema accepts (validation constraints)
//   - Tags: Custom tags that this schema introduces
//
// Example schema:
//
//	context: tony-format/context/match
//
//	signature:
//	  name: user-schema
//
//	define:
//	  user:
//	    name: !irtype ""
//	    email: !irtype ""
//	    age: .[number]
//
//	accept:
//	  .[user]
//
// ## Contexts
//
// Contexts define execution environments and which tags are available. Built-in
// contexts include:
//
//   - match: For validation/matching operations (!or, !and, !not, !type, etc.)
//   - patch: For data transformation operations (!nullify, !insert, !delete, etc.)
//   - eval: For evaluation operations (!eval, !exec, !file, etc.)
//   - diff: For diff operations (!strdiff, !arraydiff)
//
// Contexts use JSON-LD style naming with short names (e.g., "match") and full URIs
// (e.g., "tony-format/context/match").
//
// ## Tags
//
// Tags are operations or type markers that work within specific contexts. Tags
// appear on IR nodes using the `!tagName` syntax:
//
//   - Schema references: `!person` references a schema named "person"
//   - Type markers: `!irtype` marks built-in types
//   - Operations: `!or`, `!and`, `!nullify` invoke operations
//
// Tags can be composed: `!all.has-path "foo"` means "all items have path foo".
//
// ## Schema References
//
// Schemas can reference other schemas using `!schema(name)` or `!from(schema-name, def-name)`:
//
//   - `!schema example` - references schema named "example" in current context
//   - `!schema tony-format/schema/base` - references schema by full URI
//   - `!from(base-schema, number)` - references definition "number" from "base-schema"
//
// # Usage
//
// ## Parsing a Schema
//
//	import (
//	    "github.com/signadot/tony-format/go-tony/parse"
//	    "github.com/signadot/tony-format/go-tony/schema"
//	)
//
//	// Parse a Tony document into IR
//	node, err := parse.Parse(schemaBytes)
//	if err != nil {
//	    return err
//	}
//
//	// Parse the schema from IR
//	schema, err := schema.ParseSchema(node)
//	if err != nil {
//	    return err
//	}
//
// ## Using Schema Registry
//
//	import (
//	    "github.com/signadot/tony-format/go-tony/schema"
//	)
//
//	// Create a context registry
//	ctxRegistry := schema.NewContextRegistry()
//
//	// Create a schema registry
//	schemaRegistry := schema.NewSchemaRegistry(ctxRegistry)
//
//	// Register a schema
//	err := schemaRegistry.RegisterSchema(mySchema)
//	if err != nil {
//	    return err
//	}
//
//	// Resolve a schema reference
//	ref := &schema.SchemaReference{Name: "user-schema"}
//	resolved, err := schemaRegistry.ResolveSchema(ref)
//	if err != nil {
//	    return err
//	}
//
// ## Validating Documents
//
// Validation is currently not fully implemented. The Schema.Validate() method
// exists but returns an error indicating validation is not yet implemented.
//
// When implemented, validation will check documents against the schema's
// `accept` clause using match operations.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/ir - Intermediate representation
//   - github.com/signadot/tony-format/go-tony/mergeop - Match and patch operations
//   - github.com/signadot/tony-format/go-tony/parse - Parsing Tony format
package schema
