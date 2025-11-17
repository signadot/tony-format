package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/tony/debug"
	"github.com/signadot/tony-format/tony/ir"
	"github.com/signadot/tony-format/tony/libdiff"
)

var passSym = &passSymbol{name: passName}

func Pass() Symbol {
	return passSym
}

const (
	passName name = "pass"
)

type passSymbol struct {
	name
}

func (s passSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op expects no args, got %v", s, args)
	}
	return &passOp{op: op{name: s.name, child: child}}, nil
}

type passOp struct {
	op
}

func (p passOp) Patch(doc *ir.Node, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("pass op patch on %s\n", doc.Path())
	}
	return doc, nil
}

func (p passOp) Match(doc *ir.Node, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("pass op match on %s\n", doc.Path())
	}
	return true, nil
}
