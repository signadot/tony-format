// Package codegen generates Tony schemas and Go codecs from annotated Go
// types. It is the library behind the tony-codegen command.
//
// A type is annotated with a //tony: directive in its doc comment, or with an
// anonymous embedded field whose tony struct tag carries schemagen= or
// schema=:
//
//	//tony:schemagen=person,notag
//	type Person struct {
//	    Name string `tony:"field=name,required"`
//	    Age  int    `tony:"field=age,omitzero"`
//	}
//
// For each annotated type not marked codec=custom, codegen generates
// ToTonyIR, FromTonyIR, ToTony and FromTony methods, which
// [github.com/signadot/tony-format/go-tony/gomap] dispatches to. The methods
// follow the Go declaration and its field tags. By default tony-codegen
// writes them to <package>_gen.go in the package directory, and the schemas
// of the package's schemagen= types to schema_gen.tony beside it.
//
// # Directives
//
// A directive is a comma-separated list; the //tony: lines on one type
// combine into one list.
//
//   - schemagen=NAME: the type is the source of the schema NAME, which codegen
//     generates from its fields.
//   - schema=NAME: the schema NAME already exists as NAME.tony; codegen loads
//     it ([LoadSchema]) and fails when it cannot be found.
//   - notag: the generated ToTonyIR does not tag its node with !NAME.
//   - codec=custom: the type keeps its schema, and generated code for a field
//     of the type calls its ToTonyIR and FromTonyIR, but its methods are not
//     generated: the type supplies its own.
//   - context=URI: the context of the generated schema; tony-format/context
//     by default.
//   - comment=F, lineComment=F: the []string fields that carry the head and
//     line comments of the type's value.
//
// Field tags use the keys package gomap reads (field=, omit, -, field=-,
// omitzero, comment=, lineComment=), and two more: required, which makes the
// generated FromTonyIR fail when the field is absent, and optional, which
// makes ToTonyIR drop the field's zero value and the schema admit null.
//
// # Pipeline
//
// [DiscoverPackages], [ParseFile] and [ExtractTypes] find the annotated
// types; [ResolveFieldTypes] and [FlattenEmbeddedFields] resolve their fields;
// [GenerateSchema] and [WriteSchemasToSingleFile] write the generated
// schemas; [LoadSchema] loads each type's schema; [GenerateCode] produces the
// Go source.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/gomap - Encoding/decoding
//   - github.com/signadot/tony-format/go-tony/schema - Schema system
package codegen
