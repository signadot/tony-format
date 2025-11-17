package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
)

var orSym = &orSymbol{matchName: orName}

func Or() Symbol {
	return orSym
}

const (
	orName matchName = "or"
)

type orSymbol struct {
	matchName
}

func (s orSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("or op has no args, got %v", args)
	}
	return &orOp{matchOp: matchOp{op: op{name: s.matchName, child: child}}}, nil
}

type orOp struct {
	matchOp
}

func (o orOp) Match(doc *ir.Node, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("or op match on %s\n", doc.Path())
	}
	switch o.child.Type {
	case ir.ObjectType:
		if doc.Type != o.child.Type {
			return false, nil
		}
		mMap := make(map[string]*ir.Node, len(o.child.Fields))
		for i := range o.child.Fields {
			field := o.child.Fields[i]
			mMap[field.String] = o.child.Values[i]
		}
		docMap := make(map[string]*ir.Node, len(doc.Fields))
		for i := range doc.Fields {
			field := doc.Fields[i]
			docMap[field.String] = doc.Values[i]
		}
		for k, m := range mMap {
			d := docMap[k]
			if d == nil {
				continue
			}
			subMatch, err := f(d, m)
			if err != nil {
				return false, err
			}
			if subMatch {
				return true, nil
			}
		}
		return false, nil

	case ir.ArrayType:
		for _, yy := range o.child.Values {
			v, err := f(doc, yy)
			if err != nil {
				return false, err
			}
			if v {
				return true, nil
			}
		}
		return false, nil
	default:
		if doc.Type == ir.NullType {
			return false, nil
		}
		return f(doc, o.child)
	}
}
