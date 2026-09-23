// Package eval provides expression evaluation for Tony documents.
//
// Evaluates expressions and operations in the eval context, including
// !eval, !exec, !file operations.
//
// # Expressions
//
// A string may hold expressions written $[...] or .[...]. Each is evaluated with
// expr-lang (github.com/expr-lang/expr) against an environment of named values, and
// its value is written into the string in its place. Inside an expression a backslash
// escapes the next character, so \] does not close it. A string that is exactly one
// .[...] is a reference rather than text: expanding it replaces the node with the
// expression's value, which need not be a string.
//
// [ExpandString] expands the expressions in one string. [ExpandIR] expands every
// string beneath a node and answers the result, in which a reference's value keeps the
// tag the reference wore, so !not .[x] still negates. It builds the result from the
// node it is given, whose strings it expands in place, so a caller keeping that node
// passes a clone. [ExpandIRWithOptions] takes [EvalOptions] as well. [ExpandEnv]
// expands a node in place, and is what !eval does.
//
// Expressions evaluated by ExpandIR can call getpath(path) and listpath(path), which
// answer the node and the nodes at a path in the document being expanded, whereami(),
// which answers the path of the node being expanded, and getenv(name). [ToAny] and
// [FromAny] convert between nodes and the Go values expressions see. Every expression,
// with a document or without one, can call fail(msg), which answers nothing and raises
// msg as the expression's error: `cond ? value : fail("no sha")` is how an author says
// an expression has no answer, rather than defaulting to one.
//
// # When the data is not there
//
// A fetch says whether the thing it fetches from must be there, and the two spellings
// differ on purpose:
//
//	a.b       a must be there. A missing b on an object that IS there answers null;
//	          fetching b from nothing is an error, and so is a.b.c where a.b is
//	          missing -- the error is the fetch ON nothing, not the absent b.
//	a?.b      a need not be there: absent anywhere along the path answers null, and
//	          `a?.b?.c ?? "d"` is one spelling that holds however far the path got.
//	x ?? y    y when x is null, which covers a missing leaf under a present parent
//	          but not a fetch on nothing -- that is an error before ?? is reached.
//	"b" in a  whether a HOLDS b, which tells an absent b from one written null;
//	          `a.b == nil` cannot, and this answers false rather than erroring when
//	          a itself is nothing.
//
// So an author who wants a default writes ?. with ??, one who wants a demand writes .
// with fail(), and one who wants to know which it is writes in.
//
// # Operations
//
// The eval operations are !eval, !file, !exec, !script(as), !osenv, !tostring, !toint,
// !tovalue and !b64enc. Each is a [Symbol] in this package's registry ([Lookup],
// [Symbols], [Register]), whose [Op] answers the node that replaces the tagged one, and
// [SplitChild] finds the first of them in a node's tag. tony.Tool runs them over a
// document, evaluating what is beneath a tagged node before the node itself; it is what
// `o eval` does.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony - Tool, which runs the eval operations
//   - github.com/signadot/tony-format/go-tony/mergeop - Operation system
//   - github.com/signadot/tony-format/go-tony/schema - Schema contexts
package eval
