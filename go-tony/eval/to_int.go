package eval

import (
	"fmt"
	"strconv"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
)

var toIntSym = &toIntSymbol{name: toIntName}

func ToInt() Symbol {
	return toIntSym
}

const (
	toIntName name = "toint"
)

type toIntSymbol struct {
	name
}

func (s toIntSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op expects no args, got %v", s, args)
	}
	return &toIntOp{op: op{name: s.name, child: child}}, nil
}

type toIntOp struct {
	op
}

func (p toIntOp) Eval(doc *ir.Node, env Env, ef EvalFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("toInt on %s which has tag %q\n", doc.Path(), doc.Tag)
	}
	if err := ExpandEnv(doc, env); err != nil {
		return nil, err
	}
	switch doc.Type {
	case ir.NumberType:
		if doc.Int64 != nil {
			return doc, nil
		}
		doc = doc.Clone()
		if doc.Float64 != nil {
			i := int64(*doc.Float64)
			doc.Int64 = &i
			doc.Float64 = nil
		}
		i, err := strconv.ParseInt(string(doc.Number), 10, 64)
		if err != nil {
			return nil, err
		}
		doc.Int64 = &i
		return doc, nil
	case ir.BoolType:
		if doc.Bool {
			return ir.FromInt(1), nil
		}
		return ir.FromInt(0), nil
	case ir.StringType:
		i, err := strconv.ParseInt(doc.String, 10, 64)
		if err != nil {
			return nil, err
		}
		return ir.FromInt(i), nil
	default:
		return nil, fmt.Errorf("cannot translate type %s to int at %s", doc.Type, doc.Path())
	}
}
