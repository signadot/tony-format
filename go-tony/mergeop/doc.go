// Package mergeop provides operations for matching and patching Tony format documents.
//
// # Overview
//
// Merge operations are the core mechanism for working with Tony documents.
// Operations are invoked via tags (e.g., `!or`, `!and`, `!nullify`) and work
// on the intermediate representation (ir.Node) of documents.
//
// Operations are divided into two categories:
//
//   - Match operations: Validate or query documents (return boolean)
//   - Patch operations: Transform documents (return modified document)
//
// # Contexts
//
// Operations belong to execution contexts that define where they can be used:
//
//   - Match context: Validation/querying operations (!or, !and, !not, !type, !glob, etc.)
//   - Patch context: Transformation operations (!nullify, !insert, !delete, !replace, etc.)
//   - Eval context: Evaluation operations (!eval, !exec, !file, etc.)
//   - Diff context: Diff operations (!strdiff, !arraydiff)
//
// # Match Operations
//
// Match operations validate or query documents. They implement the Op interface
// and return a boolean indicating whether the match succeeded.
//
// Common match operations:
//
//   - !or: Match if any child matches
//   - !and: Match if all children match
//   - !not: Match if child does not match
//   - !type: Match if node type matches
//   - !glob: Match string against glob pattern
//   - !field: Match field name
//   - !tag: Match tag name
//   - !subtree: Recursively search subtree
//   - !all: Match all items in array/object
//   - !has-path: Check if path exists
//
// Example:
//
//	// Match operation: check if kind is ConfigMap or Secret
//	matchNode := &ir.Node{
//	    Tag: "!or",
//	    Type: ir.ArrayType,
//	    Values: []*ir.Node{
//	        ir.FromString("ConfigMap"),
//	        ir.FromString("Secret"),
//	    },
//	}
//
//	op := mergeop.Lookup("or")
//	matched, err := op.Match(doc, matchFunc)
//
// # Patch Operations
//
// Patch operations transform documents. They implement the Op interface and
// return a modified document.
//
// Common patch operations:
//
//   - !nullify: Set value to null
//   - !insert: Insert into array/object
//   - !delete: Delete from array/object
//   - !replace: Replace value
//   - !rename: Rename field
//   - !dive: Recursively apply patches
//   - !embed: Embed matched node multiple times
//   - !pipe: Pipe through external command
//   - !jsonpatch: Apply JSON Patch operations
//
// Example:
//
//	// Patch operation: set field to null
//	patchNode := &ir.Node{
//	    Tag: "!nullify",
//	    Type: ir.NullType,
//	}
//
//	op := mergeop.Lookup("nullify")
//	patched, err := op.Patch(doc, matchFunc, patchFunc, diffFunc)
//
// # Operation Registration
//
// Operations are registered globally and can be looked up by name:
//
//	// Lookup an operation
//	op := mergeop.Lookup("or")
//	if op == nil {
//	    // operation not found
//	}
//
//	// List all registered operations
//	allOps := mergeop.Symbols()
//
//	// Register a custom operation
//	err := mergeop.Register(myCustomOp)
//
// # Operation Interface
//
// All operations implement the Op interface:
//
//	type Op interface {
//	    Match(doc *ir.Node, f MatchFunc) (bool, error)
//	    Patch(doc *ir.Node, mf MatchFunc, pf PatchFunc, df libdiff.DiffFunc) (*ir.Node, error)
//	    String() string
//	}
//
// Match operations typically only implement Match(), while patch operations
// typically only implement Patch(). The IsMatch() and IsPatch() methods
// indicate which methods are supported.
//
// # Function Types
//
// Operations use function types for composition:
//
//   - MatchFunc: Function for matching documents
//   - PatchFunc: Function for patching documents
//   - libdiff.DiffFunc: Function for computing diffs
//
// These allow operations to compose and nest, enabling complex transformations.
//
// # Tag Composition
//
// Tags can be composed to create more specific operations:
//
//   - `!all.has-path "foo"`: All items must have path "foo"
//   - `!not.or`: Negation of OR operation
//   - `!subtree.field.glob "x-*"`: Find fields matching pattern in subtree
//
// Composition is handled by parsing the tag string and creating nested
// operation structures.
//
// # Built-in Operations
//
// The package registers many built-in operations on initialization:
//
// Match operations: or, and, not, type, glob, field, tag, subtree, all, let, if,
// has-path, pass, quote, unquote
//
// Patch operations: nullify, insert, delete, replace, rename, dive, embed, pipe,
// jsonpatch, addtag, rmtag, retag, strdiff, arraydiff
//
// # Usage Example
//
//	import (
//	    "github.com/signadot/tony-format/go-tony/ir"
//	    "github.com/signadot/tony-format/go-tony/mergeop"
//	)
//
//	// Create a match operation
//	matchOp := mergeop.Lookup("or")
//	if matchOp == nil {
//	    return fmt.Errorf("or operation not found")
//	}
//
//	// Match a document
//	matched, err := matchOp.Match(doc, func(doc, pattern *ir.Node) (bool, error) {
//	    // Custom match logic
//	    return true, nil
//	})
//
//	// Create a patch operation
//	patchOp := mergeop.Lookup("nullify")
//	if patchOp == nil {
//	    return fmt.Errorf("nullify operation not found")
//	}
//
//	// Patch a document
//	patched, err := patchOp.Patch(doc, matchFunc, patchFunc, diffFunc)
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/ir - Intermediate representation
//   - github.com/signadot/tony-format/go-tony/schema - Schema system
//   - github.com/signadot/tony-format/go-tony/libdiff - Diff computation
package mergeop
