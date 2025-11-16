package mergeop

import (
	"fmt"

	"github.com/tony-format/tony/debug"
	"github.com/tony-format/tony/ir"
)

var subtreeSym = &subtreeSymbol{matchName: subtreeName}

func Subtree() Symbol {
	return subtreeSym
}

const (
	subtreeName matchName = "subtree"
)

type subtreeSymbol struct {
	matchName
}

func (s subtreeSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op has no args, got %v", s, args)
	}
	return &subtreeOp{matchOp: matchOp{op: op{name: s.matchName, child: child}}}, nil
}

type subtreeOp struct {
	matchOp
}

func (s *subtreeOp) dive(doc *ir.Node, mf MatchFunc) (bool, error) {
	res, err := mf(doc, s.child)
	if err != nil {
		return false, err
	}
	if res {
		return true, nil
	}
	switch doc.Type {
	case ir.ObjectType:
		for i := range doc.Fields {
			res, err := s.dive(doc.Values[i], mf)
			if err != nil {
				return false, err
			}
			if res {
				return true, nil
			}
		}
		return false, nil

	case ir.ArrayType:
		for i := range doc.Values {
			res, err := s.dive(doc.Values[i], mf)
			if err != nil {
				return false, err
			}
			if res {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, nil
	}
}

func (subtree subtreeOp) Match(doc *ir.Node, mf MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("subtree match called on %s\n", doc.Path())
	}
	return subtree.dive(doc, mf)
}
