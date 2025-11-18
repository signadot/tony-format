package mergeop

import (
	"errors"
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/eval"
	"github.com/signadot/tony-format/go-tony/ir"
)

var letSym = &letSymbol{matchName: letName}

func Let() Symbol {
	return letSym
}

const (
	letName matchName = "let"
)

type letSymbol struct {
	matchName
}

func (s letSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("let op has no args, got %v", args)
	}
	if child.Type != ir.ObjectType {
		return nil, errors.New("let must be an object")
	}

	// Extract let: array
	letNode := ir.Get(child, "let")
	if letNode == nil {
		return nil, errors.New("let must have 'let' field")
	}
	if letNode.Type != ir.ArrayType {
		return nil, errors.New("let field must be an array")
	}

	// Extract in: node
	inNode := ir.Get(child, "in")
	if inNode == nil {
		return nil, errors.New("let must have 'in' field")
	}

	// Parse bindings from let array
	bindings := make(map[string]*ir.Node)
	for _, bindingItem := range letNode.Values {
		varName := bindingItem.Fields[0].String
		varValue := bindingItem.Values[0]
		bindings[varName] = varValue
	}

	op := &letOp{
		matchOp:  matchOp{op: op{name: s.matchName, child: child}},
		bindings: bindings,
		in:       inNode,
	}
	return op, nil
}

type letOp struct {
	matchOp
	bindings map[string]*ir.Node
	in       *ir.Node
}

func (l letOp) Match(doc *ir.Node, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("let op match on %s\n", doc.Path())
	}

	// Build environment from bindings
	env := l.buildEnv()

	// Expand using environment expansion (expects .[var] format)
	expandedIn, err := eval.ExpandIR(l.in.Clone(), env)
	if err != nil {
		return false, fmt.Errorf("error expanding let in body: %w", err)
	}

	expandedInNode, ok := expandedIn.(*ir.Node)
	if !ok {
		return false, fmt.Errorf("expected *ir.Node from ExpandIR, got %T", expandedIn)
	}

	// Match using the expanded 'in' node
	return f(doc, expandedInNode)
}

// buildEnv creates an eval.Env from the let bindings
func (l letOp) buildEnv() eval.Env {
	env := make(eval.Env)
	for varName, varValue := range l.bindings {
		env[varName] = varValue
	}
	return env
}
