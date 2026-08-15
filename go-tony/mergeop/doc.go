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
//   - Match: Validation (!or, !and, !not, !irtype, !glob, !has-path, etc.)
//   - Patch: Transformation (!nullify, !insert, !delete, !replace, etc.)
//   - Eval: Evaluation (!eval, !exec, !file, etc.)
//   - Diff: Diffing (!strdiff, !arraydiff)
//
// # Checked and unconditional patch operations
//
// Patch operations divide on a line that is easy to miss and expensive to miss: some
// state what results, and some ASSERT something about what was already there and fail if
// it does not hold.
//
//	!replace {from: X, to: Y}   verifies the node still equals X, and errors otherwise
//	!retag(from, to)            verifies the node's tag is already !from, and errors otherwise
//
// Both read as statements of a result and behave as assertions about the previous value.
// That is exactly what a diff wants -- applying one to a document that has moved should
// not silently overwrite the move -- and exactly what a stored or re-applied patch does
// not want, since it meets a document that is expected to have changed. The unconditional
// forms are !insert (the value is what results), !delete (absence is what results), and
// !addtag / !rmtag, which are !retag's two halves without the assertion.
//
// Operations also divide on whether their result depends on what they meet. !strdiff,
// !arraydiff, !rename and !jsonpatch are relative: they re-evaluate against whatever is
// there, so the same operation applied to two different documents produces two different
// results. !pipe additionally calls out to the system, so applying it twice runs it twice.
//
// A caller that stores operations, or re-applies them to a moving base, needs both
// distinctions. See system/logd/api.StorageContext for one such restriction in practice.
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
