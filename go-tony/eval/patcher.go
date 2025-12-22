package eval

import (
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/compiler"
	"github.com/expr-lang/expr/conf"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/vm"
)

// evalWithDefCallPatch parses, patches, and evaluates an expression.
// It transforms bare identifier references to parameterized definitions
// into zero-argument function calls. This allows .[array] to automatically call
// array() to get the base definition when array is defined as array(t).
//
// Example:
//   - If "array" is a parameterized def (has parameters)
//   - The expression "array" becomes "array()"
//   - The expression "array(int)" remains "array(int)" (already a call)
func evalWithDefCallPatch(input string, env map[string]any, parameterizedDefs map[string]bool) (any, error) {
	// Parse to AST
	tree, err := parser.Parse(input)
	if err != nil {
		return nil, err
	}

	// Collect all CallNode callees first (top-down traversal)
	callees := make(map[*ast.IdentifierNode]bool)
	collectCallees(tree.Node, callees)

	// Patch identifiers that aren't callees
	patchTree(&tree.Node, parameterizedDefs, callees)

	// Compile the patched AST
	config := conf.New(env)
	program, err := compiler.Compile(tree, config)
	if err != nil {
		return nil, err
	}

	return vm.Run(program, env)
}

// collectCallees recursively collects all IdentifierNodes that are CallNode callees.
// This uses top-down traversal so we see CallNodes before their children.
func collectCallees(node ast.Node, callees map[*ast.IdentifierNode]bool) {
	if node == nil {
		return
	}

	switch n := node.(type) {
	case *ast.CallNode:
		if ident, ok := n.Callee.(*ast.IdentifierNode); ok {
			callees[ident] = true
		}
		collectCallees(n.Callee, callees)
		for _, arg := range n.Arguments {
			collectCallees(arg, callees)
		}
	case *ast.BinaryNode:
		collectCallees(n.Left, callees)
		collectCallees(n.Right, callees)
	case *ast.UnaryNode:
		collectCallees(n.Node, callees)
	case *ast.ConditionalNode:
		collectCallees(n.Cond, callees)
		collectCallees(n.Exp1, callees)
		collectCallees(n.Exp2, callees)
	case *ast.ArrayNode:
		for _, elem := range n.Nodes {
			collectCallees(elem, callees)
		}
	case *ast.MapNode:
		for _, pair := range n.Pairs {
			collectCallees(pair, callees)
		}
	case *ast.PairNode:
		collectCallees(n.Key, callees)
		collectCallees(n.Value, callees)
	case *ast.MemberNode:
		collectCallees(n.Node, callees)
		collectCallees(n.Property, callees)
	case *ast.SliceNode:
		collectCallees(n.Node, callees)
		collectCallees(n.From, callees)
		collectCallees(n.To, callees)
	case *ast.ChainNode:
		collectCallees(n.Node, callees)
	case *ast.BuiltinNode:
		for _, arg := range n.Arguments {
			collectCallees(arg, callees)
		}
	case *ast.PredicateNode:
		collectCallees(n.Node, callees)
	case *ast.VariableDeclaratorNode:
		collectCallees(n.Value, callees)
		collectCallees(n.Expr, callees)
	case *ast.SequenceNode:
		for _, stmt := range n.Nodes {
			collectCallees(stmt, callees)
		}
	}
	// IdentifierNode, StringNode, IntegerNode, FloatNode, BoolNode, NilNode, ConstantNode, PointerNode
	// don't have children to traverse
}

// patchTree recursively patches the AST, transforming bare parameterized def
// identifiers into zero-argument function calls.
func patchTree(node *ast.Node, parameterizedDefs map[string]bool, callees map[*ast.IdentifierNode]bool) {
	if node == nil || *node == nil {
		return
	}

	// Check if this is an identifier to patch
	if ident, ok := (*node).(*ast.IdentifierNode); ok {
		if !callees[ident] && parameterizedDefs[ident.Value] {
			call := &ast.CallNode{
				Callee:    ident,
				Arguments: []ast.Node{},
			}
			ast.Patch(node, call)
			return
		}
	}

	// Recurse into children
	switch n := (*node).(type) {
	case *ast.CallNode:
		patchTree(&n.Callee, parameterizedDefs, callees)
		for i := range n.Arguments {
			patchTree(&n.Arguments[i], parameterizedDefs, callees)
		}
	case *ast.BinaryNode:
		patchTree(&n.Left, parameterizedDefs, callees)
		patchTree(&n.Right, parameterizedDefs, callees)
	case *ast.UnaryNode:
		patchTree(&n.Node, parameterizedDefs, callees)
	case *ast.ConditionalNode:
		patchTree(&n.Cond, parameterizedDefs, callees)
		patchTree(&n.Exp1, parameterizedDefs, callees)
		patchTree(&n.Exp2, parameterizedDefs, callees)
	case *ast.ArrayNode:
		for i := range n.Nodes {
			patchTree(&n.Nodes[i], parameterizedDefs, callees)
		}
	case *ast.MapNode:
		for i := range n.Pairs {
			patchTree(&n.Pairs[i], parameterizedDefs, callees)
		}
	case *ast.PairNode:
		patchTree(&n.Key, parameterizedDefs, callees)
		patchTree(&n.Value, parameterizedDefs, callees)
	case *ast.MemberNode:
		patchTree(&n.Node, parameterizedDefs, callees)
		patchTree(&n.Property, parameterizedDefs, callees)
	case *ast.SliceNode:
		patchTree(&n.Node, parameterizedDefs, callees)
		patchTree(&n.From, parameterizedDefs, callees)
		patchTree(&n.To, parameterizedDefs, callees)
	case *ast.ChainNode:
		patchTree(&n.Node, parameterizedDefs, callees)
	case *ast.BuiltinNode:
		for i := range n.Arguments {
			patchTree(&n.Arguments[i], parameterizedDefs, callees)
		}
	case *ast.PredicateNode:
		patchTree(&n.Node, parameterizedDefs, callees)
	case *ast.VariableDeclaratorNode:
		patchTree(&n.Value, parameterizedDefs, callees)
		patchTree(&n.Expr, parameterizedDefs, callees)
	case *ast.SequenceNode:
		for i := range n.Nodes {
			patchTree(&n.Nodes[i], parameterizedDefs, callees)
		}
	}
}
