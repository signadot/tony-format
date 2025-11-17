package eval

import (
	"fmt"

	"github.com/signadot/tony-format/tony/debug"
	"github.com/signadot/tony-format/tony/ir"
)

var evalSym = &evalSymbol{name: evalName}

func Eval() Symbol {
	return evalSym
}

const (
	evalName name = "eval"
)

type evalSymbol struct {
	name
}

func (s evalSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op expects no args, got %v", s, args)
	}
	return &evalOp{op: op{name: s.name, child: child}}, nil
}

type evalOp struct {
	op
}

func (p evalOp) Eval(doc *ir.Node, env Env, ef EvalFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("eval on %s\n", doc.Path())
	}
	if err := ExpandEnv(doc, env); err != nil {
		return nil, err
	}
	return doc, nil
}
