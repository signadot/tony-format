package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/tony/debug"
	"github.com/signadot/tony-format/tony/ir"
)

var andSym = &andSymbol{matchName: andName}

func And() Symbol {
	return andSym
}

const (
	andName matchName = "and"
)

type andSymbol struct {
	matchName
}

func (s andSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("and op has no args, got %v", args)
	}
	return &andOp{matchOp: matchOp{op: op{name: s.matchName, child: child}}}, nil
}

type andOp struct {
	matchOp
}

func (a andOp) Match(doc *ir.Node, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("and op called on %s\n", doc.Path())
	}
	switch a.child.Type {
	case ir.ArrayType:
		for _, yy := range a.child.Values {
			v, err := f(doc, yy)
			if err != nil {
				return false, err
			}
			if !v {
				return false, nil
			}
		}
		return true, nil
	default:
		return f(doc, a.child)
	}
}
