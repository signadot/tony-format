package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/tony/debug"
	"github.com/signadot/tony-format/tony/ir"
)

var notSym = &notSymbol{matchName: notName}

func Not() Symbol {
	return notSym
}

const (
	notName matchName = "not"
)

type notSymbol struct {
	matchName
}

func (s notSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("not op has no args, got %v", args)
	}
	return &notOp{matchOp: matchOp{op: op{name: s.matchName, child: child}}}, nil
}

type notOp struct {
	matchOp
}

func (n notOp) Match(doc *ir.Node, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("not op match on %s\n", doc.Path())
	}
	subMatch, err := f(doc, n.child)
	if err != nil {
		return false, err
	}
	return !subMatch, nil
}
