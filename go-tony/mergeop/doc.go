// Package mergeop provides match and patch operations for Tony documents.
//
// Operations are invoked via tags (e.g., !or, !and, !nullify) and work on
// ir.Node trees. Operations fall into two categories:
//   - Match: Validate/query documents (return bool)
//   - Patch: Transform documents (return modified node)
//
// # Contexts
//
// Operations belong to execution contexts:
//   - Match: Validation (!or, !and, !not, !type, !glob, !has-path, etc.)
//   - Patch: Transformation (!nullify, !insert, !delete, !replace, etc.)
//   - Eval: Evaluation (!eval, !exec, !file, etc.)
//   - Diff: Diffing (!strdiff, !arraydiff)
//
// # Match Operations
//
// Match operations validate or query documents:
//
//	// Check if kind is ConfigMap or Secret
//	matchNode := &ir.Node{
//	    Tag: "!or",
//	    Type: ir.ArrayType,
//	    Values: []*ir.Node{
//	        ir.FromString("ConfigMap"),
//	        ir.FromString("Secret"),
//	    },
//	}
//	op := mergeop.Lookup("or")
//	matched, _ := op.Match(doc, matchFunc)
//
// # Patch Operations
//
// Patch operations transform documents:
//
//	// Set field to null
//	patchNode := &ir.Node{Tag: "!nullify", Type: ir.NullType}
//	op := mergeop.Lookup("nullify")
//	patched, _ := op.Patch(doc, matchFunc, patchFunc, diffFunc)
//
// # Tag Composition
//
// Tags compose to create specific operations:
//   - !all.has-path "foo": All items must have path "foo"
//   - !not.or: Negation of OR
//   - !subtree.field.glob "x-*": Find matching fields in subtree
//
// # Operation Interface
//
//	type Op interface {
//	    Match(doc *ir.Node, f MatchFunc) (bool, error)
//	    Patch(doc *ir.Node, mf MatchFunc, pf PatchFunc, df DiffFunc) (*ir.Node, error)
//	    String() string
//	}
//
// Operations implement Match() or Patch() or both. Use IsMatch() and IsPatch()
// to check which are supported.
//
// # Registration
//
//	op := mergeop.Lookup("or")       // Lookup by name
//	allOps := mergeop.Symbols()      // List all operations
//	mergeop.Register(myCustomOp)     // Register custom operation
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/ir - IR representation
//   - github.com/signadot/tony-format/go-tony/schema - Schema system
package mergeop
