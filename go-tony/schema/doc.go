// Package schema provides Tony Schema for describing and validating Tony documents.
//
// Tony Schema is simpler and more readable than JSON Schema, enabling precise
// data modeling, validation, documentation, and code generation.
//
// # Schema Structure
//
// A schema contains:
//   - Context: A JSON-LD style context (a URI, an object mapping short names
//     to URIs, or a list of these); when absent, tony-format/context, under
//     which the match, patch, eval, diff, encoding and schema contexts are named
//   - Signature: Schema name and optional parameters
//   - Define: Value definitions (like JSON Schema $defs)
//   - Accept: Validation constraints
//   - Tags: Custom tags introduced by this schema
//
// Example:
//
//	context: tony-format/context
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
// # Base Definitions
//
// .[number] above refers to a base definition, from the tony-base schema in
// base.tony: string, number, int, float, bool and null, and parameterized
// definitions such as array(t), object(t) and nullable(t), which a reference
// instantiates: .[array(string)]. [MergeBaseDefinitions] adds them to a
// schema node before [ParseSchema] reads it; a schema parsed without it has
// only the definitions it makes itself.
//
// # Contexts
//
// Contexts define execution environments:
//   - match: Validation (!or, !and, !not, !irtype, etc.)
//   - patch: Transformation (!nullify, !insert, !delete, etc.)
//   - eval: Evaluation (!eval, !exec, !file, etc.)
//   - diff: Diffing (!strdiff, !arraydiff)
//
// # Tags
//
// Tags invoke operations or mark types using !tagName syntax.
// Schema references: !schema(name), !from(schema,def).
// Tags compose: !all.has-path "foo".
//
// # Usage
//
//	// Parse schema, with the base definitions
//	node, _ := parse.Parse(schemaBytes)
//	schema.MergeBaseDefinitions(node)
//	s, _ := schema.ParseSchema(node)
//
//	// Create registries
//	ctxReg := schema.NewContextRegistry()
//	schemaReg := schema.NewSchemaRegistry(ctxReg)
//	schemaReg.RegisterSchema(s)
//
//	// Resolve references
//	ref := &schema.SchemaReference{Name: "user-schema"}
//	resolved, _ := schemaReg.ResolveSchema(ref)
//
// # Validation
//
// Schemas validate documents using the Accept pattern:
//
//	// Parse and validate
//	schemaNode, _ := parse.Parse(schemaBytes)
//	schema.MergeBaseDefinitions(schemaNode)
//	s, _ := schema.ParseSchema(schemaNode)
//
//	docNode, _ := parse.Parse(docBytes)
//	err := s.Validate(docNode)
//	if err != nil {
//	    // Document does not match schema
//	}
//
// The Accept pattern supports definition references (.[defName]) and
// match operators (!and, !or, !not, !irtype, !glob, etc.).
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/ir - IR representation
//   - github.com/signadot/tony-format/go-tony/mergeop - Match/patch operations
package schema
