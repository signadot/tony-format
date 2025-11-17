package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/tony/debug"
	"github.com/signadot/tony-format/tony/encode"
	"github.com/signadot/tony-format/tony/ir"

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

func getRaw(v string) string {
	if !isRawEnvRef(v) {
		return ""
	}
	return v[2 : len(v)-1]
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
	default:
		return x, nil
	}
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
