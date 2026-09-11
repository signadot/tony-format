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
// [FromAny] convert between nodes and the Go values expressions see.
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
