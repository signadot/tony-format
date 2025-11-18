package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
)

var typeSym = &typeSymbol{matchName: typeName}

func Type() Symbol {
	return typeSym
}

const (
	typeName matchName = "irtype"
)

type typeSymbol struct {
	matchName
}

func (s typeSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("type op has no args, got %v", args)
	}
	return &typeOp{matchOp: matchOp{op: op{name: s.matchName, child: child}}}, nil
}

type typeOp struct {
	matchOp
}

func (t typeOp) Match(doc *ir.Node, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("type op match on %s\n", doc.Path())
	}
	return doc.Type == t.child.Type, nil
}
