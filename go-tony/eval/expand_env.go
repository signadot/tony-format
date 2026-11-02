package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/compiler"
	"github.com/expr-lang/expr/conf"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/vm"
)

// EvalOptions configures expression evaluation behavior.
type EvalOptions struct {
	// ParameterizedDefs is the set of definition names that have parameters.
	// When an identifier matches one of these names, it gets transformed to
	// a zero-arg function call. This allows .[array] to automatically call
	// array() to get the base definition when array is defined as array(t).
	ParameterizedDefs map[string]bool

	// Node is the current node being evaluated. When set, script functions
	// like getpath(), whereami(), listpath(), and getenv() become available
	// in .[...] expressions. The node's Root() is used to resolve paths.
	Node *ir.Node

	// DisableBuiltins names functions an expression may not call, expr-lang's
	// builtins and this package's alike. An embedder that expands a string more
	// than once and needs the same text each time disables now()
	// (xxn12a3gh12krfg8x5n0).
	DisableBuiltins []string

	// Composite says how an object or a list is written into a string: its JSON,
	// which is the default; its tony; or not at all.
	Composite CompositeForm
}

// CompositeForm is how an object or a list is written into a string
// (EvalOptions.Composite). A string that is one .[...] is not written into
// anything, and keeps its value whatever this says.
type CompositeForm int

const (
	// CompositeJSON writes a composite as its JSON.
	CompositeJSON CompositeForm = iota
	// CompositeTony writes a composite as its tony, on one line: {x: 1}, [1 2].
	CompositeTony
	// CompositeRefuse writes no composite: the expansion is an error.
	CompositeRefuse
)

// at is opts for expanding node: the same options, with node as the document
// position, for the script functions that read it.
func (opts *EvalOptions) at(node *ir.Node) *EvalOptions {
	at := EvalOptions{}
	if opts != nil {
		at = *opts
	}
	at.Node = node
	return &at
}

// evalWithOptions compiles and runs an expression under opts.
func evalWithOptions(input string, env Env, opts *EvalOptions) (any, error) {
	program, err := compile(input, env, opts)
	if err != nil {
		return nil, err
	}
	return vm.Run(program, map[string]any(env))
}

// compile compiles an expression under opts. A call of a disabled function is refused
// by a walk of the tree (refusing). An expression under a parameterized definition is
// rewritten between parsing and compiling, so it goes through the steps expr.Compile
// takes, one at a time; the rewrite takes the script funcs too, since without them it
// answered no whereami() or getpath() at all under a schema's options
// (addsgv1yh12kszdxmdn0).
func compile(input string, env Env, opts *EvalOptions) (*vm.Program, error) {
	compileOpts := baseOpts()
	var refused *refusing
	if opts != nil {
		if opts.Node != nil {
			// The node itself, not its root, so whereami() answers its path; getpath()
			// goes to the root on its own.
			compileOpts = exprOpts(opts.Node)
		}
		if len(opts.DisableBuiltins) > 0 {
			refused = newRefusing(opts.DisableBuiltins)
		}
	}
	if opts == nil || len(opts.ParameterizedDefs) == 0 {
		if refused != nil {
			compileOpts = append(compileOpts, expr.Patch(refused))
		}
		program, err := expr.Compile(input, compileOpts...)
		if rerr := refused.err(); rerr != nil {
			return nil, rerr
		}
		return program, err
	}
	// The program is specialized to the environment's Go type, so what it is compiled
	// against and run with is the plain map, not the named Env.
	config := conf.New(map[string]any(env))
	for _, opt := range compileOpts {
		opt(config)
	}
	config.Check()
	tree, err := parser.ParseWithConfig(input, config)
	if err != nil {
		return nil, err
	}
	if err := refused.walk(tree.Node); err != nil {
		return nil, err
	}
	callees := make(map[*ast.IdentifierNode]bool)
	collectCallees(tree.Node, callees)
	patchTree(&tree.Node, opts.ParameterizedDefs, callees)
	return compiler.Compile(tree, config)
}

func ExpandEnv(node *ir.Node, env Env) error {
	// Once: the block was here twice, so a comment whose expansion held $[...] was
	// expanded again (addsgv1yh12kszdxmdn0).
	if node.Comment != nil {
		for i, ln := range node.Comment.Lines {
			lnEval, err := ExpandString(ln, env)
			if err != nil {
				return fmt.Errorf("error expanding comment line %q: %w", ln, err)
			}
			node.Comment.Lines[i] = lnEval
		}
	}
	switch node.Type {
	case ir.CommentType:
		// A head comment holds the node it heads: its lines are expanded as a line
		// comment's are, and the node like any child. Neither was reached, so a
		// string under a comment stayed as written (addsgv1yh12kszdxmdn0).
		for i, ln := range node.Lines {
			lnEval, err := ExpandString(ln, env)
			if err != nil {
				return fmt.Errorf("error expanding comment line %q: %w", ln, err)
			}
			node.Lines[i] = lnEval
		}
		for _, cy := range node.Values {
			if err := ExpandEnv(cy, env); err != nil {
				return err
			}
		}
	case ir.ObjectType, ir.ArrayType:
		// A child's error is the document's: it was dropped, and the child left as
		// written in a document that reported success (addsgv1yh12kszdxmdn0).
		for _, cy := range node.Values {
			if err := ExpandEnv(cy, env); err != nil {
				return err
			}
		}
	case ir.StringType:
		raw := getRaw(node.String)
		if raw == "" {
			v, err := ExpandString(node.String, env)
			if err != nil {
				return fmt.Errorf("error expanding %q: %w", node.String, err)
			}
			node.String = v
			return nil
		}
		// Compile with script funcs (getpath, whereami, etc.)
		// Pass the node itself so whereami() returns the correct path
		// getpath() internally calls doc.Root() to resolve paths
		program, err := expr.Compile(raw, exprOpts(node)...)
		if err != nil {
			return fmt.Errorf("error compiling %q: %w", raw, err)
		}
		val, err := vm.Run(program, env)
		if err != nil {
			return fmt.Errorf("error evaluating %q: %w", raw, err)
		}
		repl, err := FromAny(val)
		if err != nil {
			return fmt.Errorf("could not translate evaluation result to Y: %w", err)
		}
		if repl != nil {
			repl.Parent = node.Parent
			repl.ParentIndex = node.ParentIndex
			repl.ParentField = node.ParentField
			*node = *repl
		} else {
			*node = *ir.Null()
		}
	}
	return nil
}

// GetRaw extracts the name from a .[name] reference syntax.
// It returns the name without the ".[" prefix and "]" suffix.
// If the string is not in .[name] format, it returns an empty string.
// Example: GetRaw(".[number]") returns "number"
func GetRaw(v string) string {
	if !isRawEnvRef(v) {
		return ""
	}
	return v[2 : len(v)-1]
}

func getRaw(v string) string {
	return GetRaw(v)
}

func ExpandIR(node *ir.Node, env map[string]any) (*ir.Node, error) {
	return ExpandIRWithOptions(node, env, nil)
}

// retagRef puts back the tag the reference itself was wearing.
//
// A .[var] reference is a whole node, and expanding it REPLACES that node with
// the bound value -- so anything the reference wore is on the node being
// discarded. `!not .[tip]` is a String node ".[tip]" tagged !not, and without
// this the negation left with the node: the match came back with the opposite
// verdict and no error, which is worse than failing, since !let is the natural
// way to write "this field differs from that value" and the differs half was the
// half that lied. The same shape covers !glob .[pat], !irtype .[t] and every other
// operator worn over a reference.
//
// The value's own tag is kept underneath rather than overwritten, so a !not over
// a value that is itself a !glob composes to !not.glob -- what the two operators
// spelled in one place would mean. The object and array branches can assign the
// tag directly instead, because what they build carries none of its own.
func retagRef(repl *ir.Node, tag string) *ir.Node {
	if tag == "" || repl == nil {
		return repl
	}
	repl.Tag = ir.TagCompose(tag, nil, repl.Tag)
	return repl
}

// ExpandIRWithOptions expands an IR node with optional evaluation options.
// This is the main entry point for schema evaluation where parameterized
// definition auto-calling is needed.
func ExpandIRWithOptions(node *ir.Node, env map[string]any, opts *EvalOptions) (*ir.Node, error) {
	// Each node is expanded under the options with itself as the position, for the
	// script funcs (whereami, getpath, etc.).
	nodeOpts := opts.at(node)

	switch node.Type {
	case ir.ObjectType:
		n := len(node.Values)
		kvs := make([]ir.KeyVal, n)
		for i, elt := range node.Values {
			f := node.Fields[i]
			xc, err := ExpandIRWithOptions(elt, env, nodeOpts)
			if err != nil {
				return nil, err
			}
			// Preserve merge keys (null-typed keys) by using the original field node
			if f.Type == ir.NullType {
				kvs[i] = ir.KeyVal{Key: nil, Val: xc}
			} else {
				kvs[i] = ir.KeyVal{Key: f, Val: xc}
			}
		}
		return withLineComment(ir.FromKeyVals(kvs).WithTag(node.Tag), node), nil
	case ir.ArrayType:
		n := len(node.Values)
		res := make([]*ir.Node, n)
		for i, elt := range node.Values {
			xc, err := ExpandIRWithOptions(elt, env, nodeOpts)
			if err != nil {
				return nil, err
			}
			res[i] = xc
		}
		return withLineComment(ir.FromSlice(res).WithTag(node.Tag), node), nil
	case ir.StringType:
		// Check for raw env refs (.[var]) - these should replace the node, not just expand the string
		raw := getRaw(node.String)
		if raw != "" {
			val, err := evalWithOptions(raw, env, nodeOpts)
			if err != nil {
				return nil, fmt.Errorf("error evaluating %q: %w", raw, err)
			}
			// If the result is already an *ir.Node, clone it and recursively expand
			// to handle nested definition references
			if nodeResult, ok := val.(*ir.Node); ok {
				// A typed nil is what getpath answers for a path the document does
				// not have, and its Clone is a nil dereference: as a value it is null
				// (4ynqp7wqh12krg32msn0 item 21).
				repl := ir.Null()
				if nodeResult != nil {
					repl = nodeResult.Clone()
					// Recursively expand the result to handle nested .[ref] patterns
					repl, err = ExpandIRWithOptions(repl, env, nodeOpts)
					if err != nil {
						return nil, fmt.Errorf("error expanding definition %q: %w", raw, err)
					}
				}
				repl.Parent = node.Parent
				repl.ParentIndex = node.ParentIndex
				repl.ParentField = node.ParentField
				return retagRef(repl, node.Tag), nil
			}
			// Otherwise convert using FromJSONAny (which handles *ir.Node and []*ir.Node)
			repl, err := FromAny(val)
			if err != nil {
				return nil, fmt.Errorf("could not translate evaluation result: %w", err)
			}
			if repl == nil {
				repl = ir.Null()
			}
			// Preserve parent relationships from the original node
			repl.Parent = node.Parent
			repl.ParentIndex = node.ParentIndex
			repl.ParentField = node.ParentField
			return retagRef(repl, node.Tag), nil
		}
		xs, err := ExpandStringWithOptions(node.String, env, nodeOpts)
		if err != nil {
			return nil, err
		}
		node.String = xs
		return node, nil
	case ir.NumberType, ir.BoolType, ir.NullType:
		if err := expandLineComment(node, env, nodeOpts); err != nil {
			return nil, err
		}
		return node, nil
	case ir.CommentType:
		inner, err := ExpandIRWithOptions(node.Values[0], env, nodeOpts)
		if err != nil {
			return nil, err
		}
		res := &ir.Node{
			Type:   ir.CommentType,
			Values: []*ir.Node{inner},
		}
		for _, ln := range node.Lines {
			xLn, err := ExpandStringWithOptions(ln, env, nodeOpts)
			if err != nil {
				return nil, err
			}
			res.Lines = append(res.Lines, xLn)
		}
		inner.Parent = res
		return res, nil
	}
	return nil, nil
}

func ExpandLineComment(node *ir.Node, env Env) error {
	return expandLineComment(node, env, nil)
}

// expandLineComment expands the expressions in node's line comment under opts.
func expandLineComment(node *ir.Node, env Env, opts *EvalOptions) error {
	if node.Comment == nil {
		return nil
	}

	xc := &ir.Node{Type: ir.CommentType, Parent: node}
	for _, ln := range node.Comment.Lines {
		xLn, err := ExpandStringWithOptions(ln, env, opts)
		if err != nil {
			return err
		}
		xc.Lines = append(xc.Lines, xLn)
	}
	node.Comment = xc
	return nil
}

// ExpandString expands $[...] and .[...] expressions in a string.
//
// Expressions are evaluated using expr-lang against the provided environment. An
// expression ends at the ] that closes it: a [ opened inside it is closed by its own ],
// and a ] in a quoted string closes nothing, so items[5] and m["]"] are written as they
// are. Within expressions, backslash escaping is supported:
//   - \] → literal ] (does not close the expression)
//   - \\ → literal \
//   - \x → x (for any character x)
//
// Outside an expression, $$[ is a literal $[; $$ before anything else is as written.
//
// If an expression is not closed with an unescaped ], the text is treated
// as a literal string rather than an expression.
func ExpandString(v string, env map[string]any) (string, error) {
	return ExpandStringWithOptions(v, env, nil)
}

// ExpandStringWithOptions expands the expressions in v as ExpandString does, under opts.
func ExpandStringWithOptions(v string, env map[string]any, opts *EvalOptions) (string, error) {
	var out []byte
	for i := 0; i < len(v); {
		if strings.HasPrefix(v[i:], "$$[") {
			out = append(out, "$["...)
			i += 3
			continue
		}
		if (v[i] != '$' && v[i] != '.') || !strings.HasPrefix(v[i+1:], "[") {
			out = append(out, v[i])
			i++
			continue
		}
		key, end, ok := scanExpression(v, i+2, true)
		if !ok {
			// Nothing closes it: literal, to the end.
			out = append(out, v[i:]...)
			break
		}
		key = strings.TrimSpace(key)
		x, err := evalWithOptions(key, env, opts)
		if err != nil {
			return "", fmt.Errorf("error evaluating %q: %w", key, err)
		}
		if debug.Eval() {
			debug.Logf("eval %q gave %#v\n", key, x)
		}
		anyBytes, err := anyToBytes(x, opts)
		if err != nil {
			return "", fmt.Errorf("could not marshal evaluation results for %s: %w", key, err)
		}
		out = append(out, anyBytes...)
		i = end
	}
	return string(out), nil
}

// scanExpression reads the expression that starts at v[from] up to the ] that closes
// it, and answers its text with the escapes undone, and where the text after the ]
// begins. It answers false when nothing closes it.
//
// With quotes, a ] in a quoted string closes nothing. A quote nothing closes would
// then take the ] with it, and a typo in a string went through as literal text where
// it used to be refused: so the expression is read again without quotes, and ends at
// the first ], as it always did.
func scanExpression(v string, from int, quotes bool) (string, int, bool) {
	var key []byte
	depth := 0
	var quote byte
	for i := from; i < len(v); i++ {
		c := v[i]
		switch {
		case c == '\\':
			if i+1 < len(v) {
				key = append(key, v[i+1])
				i++
			}
		case quote != 0:
			if c == quote {
				quote = 0
			}
			key = append(key, c)
		case quotes && (c == '"' || c == '\'' || c == '`'):
			quote = c
			key = append(key, c)
		case c == '[':
			depth++
			key = append(key, c)
		case c == ']' && depth > 0:
			depth--
			key = append(key, c)
		case c == ']':
			return string(key), i + 1, true
		default:
			key = append(key, c)
		}
	}
	if quote != 0 {
		return scanExpression(v, from, false)
	}
	return "", 0, false
}

// anyToBytes is the text a value is written into a string as. A scalar is its value;
// a composite is written as opts.Composite says.
func anyToBytes(v any, opts *EvalOptions) ([]byte, error) {
	switch x := v.(type) {
	case nil:
		return []byte("null"), nil
	case string:
		return []byte(x), nil
	case float64:
		return []byte(strconv.FormatFloat(x, 'f', -1, 64)), nil
	case bool:
		return []byte(strconv.FormatBool(x)), nil
	case json.Number:
		return []byte(x), nil
	case *ir.Node:
		// A typed nil is what getpath answers for a path the document does not
		// have, and its Type is a nil dereference: as a value it is null.
		if x == nil {
			return []byte("null"), nil
		}
		// A scalar node is its value, as the scalar cases above write theirs.
		switch x.Type {
		case ir.StringType:
			return []byte(x.String), nil
		case ir.NullType:
			return []byte("null"), nil
		case ir.ObjectType, ir.ArrayType:
			return compositeBytes(x, x.Type == ir.ObjectType, opts)
		}
		return MarshalJSON(x)
	}
	switch kind := reflect.TypeOf(v).Kind(); kind {
	case reflect.Map, reflect.Slice, reflect.Array:
		return compositeBytes(v, kind == reflect.Map, opts)
	}
	return json.Marshal(v)
}

// compositeBytes writes an object or a list into a string as opts.Composite says. The
// default is its JSON (docs/build-eval.md, String Expansion): it was written as tony
// wire text, and the block form for a Go value (p478tacqh12krg32msn0 item 19).
func compositeBytes(v any, object bool, opts *EvalOptions) ([]byte, error) {
	form := CompositeJSON
	if opts != nil {
		form = opts.Composite
	}
	switch form {
	case CompositeTony:
		node, err := FromAny(v)
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := encode.Encode(node, &buf, encode.EncodeWire(true)); err != nil {
			return nil, err
		}
		return bytes.TrimRight(buf.Bytes(), "\n"), nil
	case CompositeRefuse:
		if object {
			return nil, fmt.Errorf("an object has no text form")
		}
		return nil, fmt.Errorf("a list has no text form")
	}
	if node, ok := v.(*ir.Node); ok {
		return MarshalJSON(node)
	}
	return json.Marshal(v)
}

func isRawEnvRef(s string) bool {
	return strings.HasPrefix(s, ".[") && strings.HasSuffix(s, "]")
}

// withLineComment carries from's line comment onto to, a container rebuilt from
// it. The rebuild kept the tag and dropped the comment, so `a: # why` over a block
// lost its why through every expansion (p478tacqh12krg32msn0 item 19).
func withLineComment(to, from *ir.Node) *ir.Node {
	if from.Comment == nil {
		return to
	}
	c := from.Comment.Clone()
	c.Parent = to
	to.Comment = c
	return to
}
