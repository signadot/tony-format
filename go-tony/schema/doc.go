// Package schema provides Tony Schema for describing and validating Tony documents.
//
// Tony Schema is simpler and more readable than JSON Schema, enabling precise
// data modeling, validation, documentation, and code generation.
//
// # Schema Structure
//
// A schema contains:
//   - Context: Execution context (match, patch, eval, etc.)
//   - Signature: Schema name and optional parameters
//   - Define: Value definitions (like JSON Schema $defs)
//   - Accept: Validation constraints
//   - Tags: Custom tags introduced by this schema
//
// Example:
//
//	context: match
//	signature:
//	  name: user-schema
//	define:
//	  user:
//	    name: !irtype ""
//	    email: !irtype ""
//	    age: .[number]
//	accept:
//	  .[user]
//
// # Contexts
//
// Contexts define execution environments:
//   - match: Validation (!or, !and, !not, !type, etc.)
//   - patch: Transformation (!nullify, !insert, !delete, etc.)
//   - eval: Evaluation (!eval, !exec, !file, etc.)
//   - diff: Diffing (!strdiff, !arraydiff)
//
// # Tags
//
// Tags invoke operations or mark types using !tagName syntax.
// Schema references: !schema(name), !from(schema, def).
// Tags compose: !all.has-path "foo".
//
// # Usage
//
//	// Parse schema
//	node, _ := parse.Parse(schemaBytes)
//	schema, _ := schema.ParseSchema(node)
//
//	// Create registries
//	ctxReg := schema.NewContextRegistry()
//	schemaReg := schema.NewSchemaRegistry(ctxReg)
//	schemaReg.RegisterSchema(mySchema)
//
//	// Resolve references
//	ref := &schema.SchemaReference{Name: "user-schema"}
//	resolved, _ := schemaReg.ResolveSchema(ref)
//
// Validation is not yet implemented.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/ir - IR representation
//   - github.com/signadot/tony-format/go-tony/mergeop - Match/patch operations
package schema
