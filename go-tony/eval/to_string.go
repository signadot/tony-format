package eval

import (
	"bytes"
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
)

var toStringSym = &toStringSymbol{name: toStringName}

func ToString() Symbol {
	return toStringSym
}

const (
	toStringName name = "tostring"
)

type toStringSymbol struct {
	name
}

func (s toStringSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op expects no args, got %v", s, args)
	}
	return &toStringOp{op: op{name: s.name, child: child}}, nil
}

type toStringOp struct {
	op
}

func (p toStringOp) Eval(doc *ir.Node, env Env, ef EvalFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("toString on %s which has tag %q\n", doc.Path(), doc.Tag)
	}
	if err := ExpandEnv(doc, env); err != nil {
		return nil, err
	}
	if doc.Type == ir.StringType {
		return doc.Clone(), nil
	}
	buf := bytes.NewBuffer(nil)
	if err := encode.Encode(doc, buf); err != nil {
		return nil, err
	}
	return ir.FromString(buf.String()), nil
}
