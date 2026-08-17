package mergeop

import (
	"errors"
	"fmt"
	"strings"

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

	// Parse bindings from let array.
	//
	// Every field of every item binds. Reading Fields[0] alone took the first
	// binding of a multi-field item and dropped the rest without saying so, and
	// read off the end of an item with no fields at all -- `let: [{}]` was a panic
	// out of a match.
	bindings := make(map[string]*ir.Node)
	for i, bindingItem := range letNode.Values {
		item := ir.Uncomment(bindingItem)
		if item == nil || item.Type != ir.ObjectType {
			return nil, fmt.Errorf("let binding %d is %s, and a binding is an object naming what it binds",
				i, bindingItemType(item))
		}
		if len(item.Fields) == 0 {
			return nil, fmt.Errorf("let binding %d binds nothing", i)
		}
		for j, f := range item.Fields {
			if j >= len(item.Values) {
				return nil, fmt.Errorf("let binding %d is malformed: %d names and %d values",
					i, len(item.Fields), len(item.Values))
			}
			bindings[f.String] = item.Values[j]
		}
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

func (l letOp) Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("let op match on %s\n", doc.Path())
	}

	// Build environment from bindings
	env := l.buildEnv()

	// A reference this let does not bind is a mistake, and it has to be said. An
	// unbound .[name] expanded to NULL, and a null pattern matches anything, so a
	// misspelt name did not fail -- it matched every document there is, which is
	// the one wrong answer worse than an error:
	//
	//	!let {let: [{t: x}], in: {sha: .[nope]}}   matched {sha: aaa111}
	//
	// The same is why a nested !let is refused rather than answered: this
	// expansion reaches the inner let's `in` and does not know its bindings, so it
	// would blank them the same way (3q8z5zvkh12kr9dpg9n0).
	if unbound := l.unboundRefs(l.in); len(unbound) > 0 {
		return false, fmt.Errorf("let does not bind %s", strings.Join(unbound, ", "))
	}

	// Expand using environment expansion (expects .[var] format)
	expandedIn, err := eval.ExpandIR(l.in.Clone(), env)
	if err != nil {
		return false, fmt.Errorf("error expanding let in body: %w", err)
	}

	// Match using the expanded 'in' node
	return f(doc, expandedIn, ctx)
}

// buildEnv creates an eval.Env from the let bindings
func (l letOp) buildEnv() eval.Env {
	env := make(eval.Env)
	for varName, varValue := range l.bindings {
		env[varName] = varValue
	}
	return env
}

// bindingItemType names what a malformed binding is, for the error.
func bindingItemType(n *ir.Node) string {
	if n == nil {
		return "nothing"
	}
	return n.Type.String()
}

// unboundRefs answers the .[name] references in n which this let does not bind,
// in the order they are met and without repeats.
//
// It is the check that turns a misspelt name from a match into an error. The
// walk is over the whole `in` body, nested lets included: this expansion reaches
// into them, so a name they mean to bind is a name this let is about to blank.
func (l letOp) unboundRefs(n *ir.Node) []string {
	seen := map[string]bool{}
	var res []string
	var walk func(*ir.Node)
	walk = func(n *ir.Node) {
		if n == nil {
			return
		}
		n = ir.Uncomment(n)
		if n == nil {
			return
		}
		if n.Type == ir.StringType {
			if name := eval.GetRaw(n.String); name != "" {
				if _, ok := l.bindings[name]; !ok && !seen[name] {
					seen[name] = true
					res = append(res, "."+"["+name+"]")
				}
			}
		}
		for _, v := range n.Values {
			walk(v)
		}
	}
	walk(n)
	return res
}
