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
)

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
		val, err := expr.Eval(raw, env)
		if err != nil {
			return fmt.Errorf("error evaluating %q: %w", raw, err)
		}
		repl, err := FromJSONAny(val)
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

func ExpandIR(node *ir.Node, env Env) (any, error) {
	switch node.Type {
	case ir.ObjectType:
		n := len(node.Values)
		res := make(map[string]*ir.Node, n)
		for i, elt := range node.Values {
			f := node.Fields[i]
			xc, err := ExpandIR(elt, env)
			if err != nil {
				return nil, err
			}
			res[f.String] = xc.(*ir.Node)
		}
		return ir.FromMap(res).WithTag(node.Tag), nil
	case ir.ArrayType:
		n := len(node.Values)
		res := make([]*ir.Node, n)
		for i, elt := range node.Values {
			xc, err := ExpandIR(elt, env)
			if err != nil {
				return nil, err
			}
			res[i] = xc.(*ir.Node)
		}
		return ir.FromSlice(res).WithTag(node.Tag), nil
	case ir.StringType:
		// Check for raw env refs (.[var]) - these should replace the node, not just expand the string
		raw := getRaw(node.String)
		if raw != "" {
			val, err := expr.Eval(raw, env)
			if err != nil {
				return nil, fmt.Errorf("error evaluating %q: %w", raw, err)
			}
			// If the result is already an *ir.Node, clone it and preserve parent relationships
			if nodeResult, ok := val.(*ir.Node); ok {
				repl := nodeResult.Clone()
				repl.Parent = node.Parent
				repl.ParentIndex = node.ParentIndex
				repl.ParentField = node.ParentField
				return repl, nil
			}
			// Otherwise convert using FromJSONAny (which handles *ir.Node and []*ir.Node)
			repl, err := FromJSONAny(val)
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
		xs, err := ExpandString(node.String, env)
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
		inner, err := ExpandIR(node.Values[0], env)
		if err != nil {
			return nil, err
		}
		irInner := inner.(*ir.Node)
		res := &ir.Node{
			Type:   ir.CommentType,
			Values: []*ir.Node{irInner},
		}
		for _, ln := range node.Lines {
			xLn, err := ExpandString(ln, env)
			if err != nil {
				return nil, err
			}
			res.Lines = append(res.Lines, xLn)
		}
		irInner.Parent = res
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

func ExpandString(v string, env Env) (string, error) {
	if len(v) < 3 {
		return v, nil
	}
	// $[x]
	j := -1
	i := 0
	n := len(v)
	var buf []byte
	for i < n-1 {
		c, next := v[i], v[i+1]
		i++
		switch c {
		case '$', '.':
			if next == '[' {
				j = i + 1
				i++
				continue
			}
			if j == -1 {
				buf = append(buf, c)
			}
		case ']':
			if j != -1 {
				key := v[j : i-1]
				x, err := expr.Eval(strings.TrimSpace(key), env)
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
				buf = append(buf, anyBytes...)
				j = -1
				continue
			}
			buf = append(buf, c)
		default:
			if j == -1 {
				buf = append(buf, c)
			}
		}
	}
	if j == -1 {
		buf = append(buf, v[n-1])
		return string(buf), nil
	}
	if v[n-1] != ']' {
		buf = append(buf, v[j-2:n]...)
	} else {
		key := v[j : n-1]
		x, err := expr.Eval(strings.TrimSpace(key), env)
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
		buf = append(buf, anyBytes...)
	}
	return string(buf), nil
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
		node, err := FromJSONAny(v)
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
