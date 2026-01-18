package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"

	"github.com/expr-lang/expr"
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
}

// evalWithOptions compiles and runs an expression with optional AST patching
// for parameterized definition auto-calling.
func evalWithOptions(input string, env Env, opts *EvalOptions) (any, error) {
	// If we have parameterized defs, use the patching path
	if opts != nil && len(opts.ParameterizedDefs) > 0 {
		return evalWithDefCallPatch(input, env, opts.ParameterizedDefs)
	}

	// Build compile options - include script funcs if we have a node
	// Pass the node itself (not Root) so whereami() returns the correct path
	// getpath() internally calls doc.Root() to resolve paths
	var compileOpts []expr.Option
	if opts != nil && opts.Node != nil {
		compileOpts = exprOpts(opts.Node)
	}

	// Compile and run
	program, err := expr.Compile(input, compileOpts...)
	if err != nil {
		return nil, err
	}

	return vm.Run(program, env)
}

func ExpandEnv(node *ir.Node, env Env) error {
	if node.Comment != nil {
		for i, ln := range node.Comment.Lines {
			lnEval, err := ExpandString(ln, env)
			if err != nil {
				return fmt.Errorf("error expanding comment %q: %w", ln, err)
			}
			node.Comment.Lines[i] = lnEval
		}
	}
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
	case ir.ObjectType:
		for i := range node.Fields {
			cy := node.Values[i]
			ExpandEnv(cy, env)
		}
	case ir.ArrayType:
		for _, cy := range node.Values {
			ExpandEnv(cy, env)
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

func ExpandAny(v any, env Env) (any, error) {
	_, isY := v.(*ir.Node)
	if isY {
		return nil, fmt.Errorf("ExpandAny is not for y.Y")
	}
	switch x := v.(type) {
	case map[int]any:
		for k := range x {
			vv, err := ExpandAny(x[k], env)
			if err != nil {
				return nil, err
			}
			x[k] = vv
		}
		return x, nil

	case map[string]any:
		for k := range x {
			vv, err := ExpandAny(x[k], env)
			if err != nil {
				return nil, err
			}
			x[k] = vv
		}
		return x, nil
	case []any:
		for i := range x {
			vv, err := ExpandAny(x[i], env)
			if err != nil {
				return nil, err
			}
			x[i] = vv
		}
		return x, nil
	case string:
		raw := getRaw(x)
		if raw == "" {
			v, err := ExpandString(x, env)
			if err != nil {
				return nil, fmt.Errorf("error expanding %q: %w", x, err)
			}
			return v, nil
		}
		val, err := expr.Eval(raw, env)
		if err != nil {
			return nil, fmt.Errorf("error evaluating %q: %w", raw, err)
		}
		return val, nil
	case *ir.Node:
		return ExpandIR(x, env)
	default:
		return x, nil
	}
}

func ExpandIR(node *ir.Node, env map[string]any) (*ir.Node, error) {
	return ExpandIRWithOptions(node, env, nil)
}

// ExpandIRWithOptions expands an IR node with optional evaluation options.
// This is the main entry point for schema evaluation where parameterized
// definition auto-calling is needed.
func ExpandIRWithOptions(node *ir.Node, env map[string]any, opts *EvalOptions) (*ir.Node, error) {
	// Create opts with current node for script funcs (whereami, getpath, etc.)
	// Each recursive call gets its own opts with the current node
	nodeOpts := &EvalOptions{Node: node}
	if opts != nil {
		nodeOpts.ParameterizedDefs = opts.ParameterizedDefs
	}

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
		return ir.FromKeyVals(kvs).WithTag(node.Tag), nil
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
		return ir.FromSlice(res).WithTag(node.Tag), nil
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
				repl := nodeResult.Clone()
				// Recursively expand the result to handle nested .[ref] patterns
				repl, err = ExpandIRWithOptions(repl, env, nodeOpts)
				if err != nil {
					return nil, fmt.Errorf("error expanding definition %q: %w", raw, err)
				}
				repl.Parent = node.Parent
				repl.ParentIndex = node.ParentIndex
				repl.ParentField = node.ParentField
				return repl, nil
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
			return repl, nil
		}
		xs, err := expandStringWithOptions(node.String, env, nodeOpts)
		if err != nil {
			return nil, err
		}
		node.String = xs
		return node, nil
	case ir.NumberType, ir.BoolType, ir.NullType:
		if err := ExpandLineComment(node, env); err != nil {
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
			xLn, err := expandStringWithOptions(ln, env, nodeOpts)
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
	if node.Comment == nil {
		return nil
	}

	xc := &ir.Node{Type: ir.CommentType, Parent: node}
	for _, ln := range node.Comment.Lines {
		xLn, err := ExpandString(ln, env)
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
// Expressions are evaluated using expr-lang against the provided environment.
// Within expressions, backslash escaping is supported:
//   - \] → literal ] (does not close the expression)
//   - \\ → literal \
//   - \x → x (for any character x)
//
// If an expression is not closed with an unescaped ], the text is treated
// as a literal string rather than an expression.
func ExpandString(v string, env map[string]any) (string, error) {
	return expandStringWithOptions(v, env, nil)
}

func expandStringWithOptions(v string, env map[string]any, opts *EvalOptions) (string, error) {
	if len(v) < 3 {
		return v, nil
	}
	// $[x] or .[x] with backslash escaping: \] -> ], \\ -> \
	exprStart := -1 // position of $ or . that starts the expression
	i := 0
	n := len(v)
	var outBuf []byte // accumulates the final output
	var keyBuf []byte // accumulates the current expression content (unescaped)

	for i < n-1 {
		c, next := v[i], v[i+1]
		i++
		switch c {
		case '$', '.':
			if next == '[' {
				exprStart = i - 1
				keyBuf = keyBuf[:0]
				i++
				continue
			}
			if exprStart == -1 {
				outBuf = append(outBuf, c)
			} else {
				keyBuf = append(keyBuf, c)
			}
		case '\\':
			if exprStart != -1 {
				// backslash escapes the next character
				keyBuf = append(keyBuf, next)
				i++
				continue
			}
			outBuf = append(outBuf, c)
		case ']':
			if exprStart != -1 {
				key := strings.TrimSpace(string(keyBuf))
				x, err := evalWithOptions(key, env, opts)
				if err != nil {
					return "", fmt.Errorf("error evaluating %q: %w", key, err)
				}
				if debug.Eval() {
					debug.Logf("eval %q gave %#v\n", key, x)
				}
				anyBytes, err := anyToBytes(x)
				if err != nil {
					return "", fmt.Errorf("could not marshal evaluation results for %s: %w", key, err)
				}
				outBuf = append(outBuf, anyBytes...)
				exprStart = -1
				continue
			}
			outBuf = append(outBuf, c)
		default:
			if exprStart == -1 {
				outBuf = append(outBuf, c)
			} else {
				keyBuf = append(keyBuf, c)
			}
		}
	}

	// Handle end of string
	if exprStart == -1 {
		outBuf = append(outBuf, v[n-1])
		return string(outBuf), nil
	}

	// Still inside expression - no unescaped ] found
	// Check if last char is ] and we didn't escape past the end
	if i >= n || v[n-1] != ']' {
		// Not a valid expression - output literally
		outBuf = append(outBuf, v[exprStart:n]...)
		return string(outBuf), nil
	}

	key := strings.TrimSpace(string(keyBuf))
	x, err := evalWithOptions(key, env, opts)
	if err != nil {
		return "", fmt.Errorf("error evaluating %q: %w", key, err)
	}
	if debug.Eval() {
		debug.Logf("eval %q gave %#v\n", key, x)
	}
	anyBytes, err := anyToBytes(x)
	if err != nil {
		return "", fmt.Errorf("could not marshal evaluation results for %s: %w", key, err)
	}
	outBuf = append(outBuf, anyBytes...)
	return string(outBuf), nil
}

func anyToBytes(v any) ([]byte, error) {
	switch x := v.(type) {
	case string:
		return []byte(x), nil
	case float64:
		return []byte(strconv.FormatFloat(x, 'f', -1, 64)), nil
	case bool:
		return []byte(strconv.FormatBool(x)), nil
	case json.Number:
		return []byte(x), nil
	case *ir.Node:
		buf := bytes.NewBuffer(nil)
		if err := encode.Encode(x, buf, encode.EncodeWire(true)); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	default:
		node, err := FromAny(v)
		if err != nil {
			return nil, err
		}
		buf := bytes.NewBuffer(nil)
		err = encode.Encode(node, buf)
		if err != nil {
			return nil, err
		}
		// d, err := yaml.Marshal(v)
		// if err != nil {
		// 	return nil, err
		// }
		d := buf.Bytes()
		return d[:len(d)-1], nil
	}
}

func isRawEnvRef(s string) bool {
	return strings.HasPrefix(s, ".[") && strings.HasSuffix(s, "]")
}
