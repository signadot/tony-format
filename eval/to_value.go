package eval

import (
	"fmt"

	"github.com/tony-format/tony/debug"
	"github.com/tony-format/tony/ir"
	"github.com/tony-format/tony/parse"
)

var toValueSym = &toValueSymbol{name: toValueName}

func ToValue() Symbol {
	return toValueSym
}

const (
	toValueName name = "tovalue"
)

type toValueSymbol struct {
	name
}

func (s toValueSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op expects no args, got %v", s, args)
	}
	if child.Type != ir.StringType {
		return nil, fmt.Errorf("tovalue only applies to strings, got %s", child.Type)
	}
	return &toValueOp{op: op{name: s.name, child: child}}, nil
}

type toValueOp struct {
	op
}

func (p toValueOp) Eval(doc *ir.Node, env Env, ef EvalFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("tovalue on %s which has tag %q\n", doc.Path(), doc.Tag)
	}
	if err := ExpandEnv(doc, env); err != nil {
		return nil, err
	}
	if doc.Type != ir.StringType {
		return nil, fmt.Errorf("toyaml only applies to strings, got %s after evaluating", doc.Type)
	}
	return parse.Parse([]byte(doc.String))
}
