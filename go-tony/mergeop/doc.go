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
//   - Eval: Evaluation (!eval, !exec, !file, etc.), whose operations are package eval's
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
// !arraydiff, !rename, !field(from,to) and !json-patch are relative: they re-evaluate
// against whatever is there, so the same operation applied to two different documents
// produces two different results. !pipe additionally calls out to the system, so
// applying it twice runs it twice.
//
// A caller that stores operations, or re-applies them to a moving base, needs both
// distinctions. See system/logd/api.StorageContext for one such restriction in practice.
//
// # Match Operations
//
// Match operations validate or query documents. tony.Match applies a pattern to a
// document, handing each tagged node of the pattern to the operation its tag names:
//
//	// kind is ConfigMap or Secret
//	pattern, err := parse.Parse([]byte(`{kind: !or [ConfigMap, Secret]}`))
//	if err != nil {
//	    return err
//	}
//	matched, err := tony.Match(doc, pattern)
//
// # Patch Operations
//
// Patch operations transform documents. tony.Patch applies a patch the same way:
//
//	// set spec to null, keeping the field
//	patch, err := parse.Parse([]byte(`{spec: !nullify null}`))
//	if err != nil {
//	    return err
//	}
//	patched, err := tony.Patch(doc, patch)
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
//	    Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error)
//	    Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, df libdiff.DiffFunc) (*ir.Node, error)
//	    String() string
//	}
//
// A [Symbol] names an operation and builds its [Op] from the tagged node's child and
// the tag's arguments. [SplitChild] finds the first tag in a node's chain that names a
// registered operation, and answers that name, its arguments and the child, which
// carries the rest of the chain. Operations implement Match or Patch or both, and the
// other answers an error; the Symbol's IsMatch and IsPatch say which. The functions an
// Op is handed are how it recurses into its operand: tony passes its own match, patch
// and diff.
//
// # Registration
//
//	sym := mergeop.Lookup("or")                      // by name without the '!'; nil if none
//	all := mergeop.Symbols()                         // every registered operation
//	err := mergeop.RegisterNamespaced("acme", mySym) // a consumer's operation, tagged !acme:<name>
//
// [Register] is for this package's built-in operations, which own every name without a
// namespace; a consumer registers with [RegisterNamespaced].
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony - Match, Patch and Diff, which apply these operations
//   - github.com/signadot/tony-format/go-tony/ir - IR representation
//   - github.com/signadot/tony-format/go-tony/schema - Schema system
package mergeop
