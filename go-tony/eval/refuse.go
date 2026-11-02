package eval

import (
	"fmt"

	"github.com/expr-lang/expr/ast"
)

// refusing refuses a call of a disabled function (EvalOptions.DisableBuiltins),
// expr-lang's builtins and this package's alike, by a walk of the expression's tree.
// A builtin that takes a predicate -- map, filter, all -- is parsed as a node of its
// own, and a function bound to its name would have changed how it parses rather than
// refused it; the walk sees both kinds.
type refusing struct {
	disabled map[string]bool
	refused  string // the first disabled name called, or none
}

func newRefusing(names []string) *refusing {
	r := &refusing{disabled: map[string]bool{}}
	for _, name := range names {
		r.disabled[name] = true
	}
	return r
}

// Visit is the ast.Visitor expr.Patch runs over the tree as it compiles.
func (r *refusing) Visit(node *ast.Node) {
	name := ""
	switch n := (*node).(type) {
	case *ast.BuiltinNode:
		name = n.Name
	case *ast.CallNode:
		if id, ok := n.Callee.(*ast.IdentifierNode); ok {
			name = id.Value
		}
	}
	if name != "" && r.disabled[name] && r.refused == "" {
		r.refused = name
	}
}

// walk refuses a call beneath node, for a tree compiled outside expr.Compile.
func (r *refusing) walk(node ast.Node) error {
	if r == nil {
		return nil
	}
	ast.Walk(&node, r)
	return r.err()
}

// err is the refusal, once the walk has run; nil when nothing was refused.
func (r *refusing) err() error {
	if r == nil || r.refused == "" {
		return nil
	}
	return fmt.Errorf("%s() is not available here", r.refused)
}
