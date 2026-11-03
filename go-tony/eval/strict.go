package eval

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/expr-lang/expr/ast"
)

// A plain path -- a name, then .field steps and constant indexes, none of them
// optional -- is looked up strictly: an absent name, field or index is an error that
// says what is absent, where the evaluator answers nil and a string shows "null".
// Nothing else in an expression changes: `a?.b`, `x ?? y`, `"b" in a`, a ternary, a
// call or a comparison evaluates as it always has, so a default stays the author's to
// write, and `a?.b` is the spelling for a b that may not be there
// (xxn12a3gh12krfg8x5n0).
//
// The check reads maps and lists. A step into anything else -- a struct, a function,
// nothing -- is left to the evaluator, whose meaning it is.

// pathStep is one step of a plain path: a field name, or a constant index.
type pathStep struct {
	key   string
	index int
	isKey bool
}

// plainPath answers the steps of node when it is a plain path, and nil otherwise.
func plainPath(node ast.Node) []pathStep {
	switch n := node.(type) {
	case *ast.IdentifierNode:
		return []pathStep{{key: n.Value, isKey: true}}
	case *ast.MemberNode:
		if n.Optional || n.Method {
			return nil
		}
		base := plainPath(n.Node)
		if base == nil {
			return nil
		}
		switch p := n.Property.(type) {
		case *ast.StringNode:
			return append(base, pathStep{key: p.Value, isKey: true})
		case *ast.IntegerNode:
			return append(base, pathStep{index: p.Value})
		case *ast.UnaryNode:
			if i, ok := p.Node.(*ast.IntegerNode); ok && p.Operator == "-" {
				return append(base, pathStep{index: -i.Value})
			}
		}
	}
	return nil
}

// pathText writes a path as an author would: a.b[0].c.
func pathText(path []pathStep) string {
	var b strings.Builder
	for i, s := range path {
		switch {
		case !s.isKey:
			fmt.Fprintf(&b, "[%d]", s.index)
		case i == 0:
			b.WriteString(s.key)
		default:
			b.WriteString("." + s.key)
		}
	}
	return b.String()
}

// lookup walks env along path and says what is absent; nil when every step is there,
// or the walk reaches something it does not read.
func lookup(env map[string]any, path []pathStep) error {
	cur := any(env)
	if path[0].key != "$env" { // the evaluator's name for the environment itself
		v, ok := env[path[0].key]
		if !ok {
			return fmt.Errorf("no such name %q", path[0].key)
		}
		cur = v
	}
	for i, step := range path[1:] {
		v := reflect.ValueOf(cur)
		for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
			if v.IsNil() {
				return nil // a fetch on nothing is the evaluator's error
			}
			v = v.Elem()
		}
		switch v.Kind() {
		case reflect.Map:
			if !step.isKey || v.Type().Key().Kind() != reflect.String {
				return nil
			}
			e := v.MapIndex(reflect.ValueOf(step.key).Convert(v.Type().Key()))
			if !e.IsValid() {
				return fmt.Errorf("no %q in %s", step.key, pathText(path[:i+1]))
			}
			cur = e.Interface()
		case reflect.Slice, reflect.Array:
			if step.isKey {
				return nil
			}
			n, idx := v.Len(), step.index
			if idx < 0 {
				idx += n
			}
			if idx < 0 || idx >= n {
				return fmt.Errorf("no index %d in %s, which has %d elements", step.index, pathText(path[:i+1]), n)
			}
			cur = v.Index(idx).Interface()
		default:
			return nil
		}
	}
	return nil
}
