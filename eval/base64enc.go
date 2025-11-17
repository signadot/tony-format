package eval

import (
	"encoding/base64"
	"fmt"

	"github.com/signadot/tony-format/tony/debug"
	"github.com/signadot/tony-format/tony/ir"
)

var b64encSym = &b64encSymbol{name: b64encName}

func B64Enc() Symbol {
	return b64encSym
}

const (
	b64encName name = "b64enc"
)

type b64encSymbol struct {
	name
}

func (s b64encSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op expects no args, got %v", s, args)
	}
	if child.Type != ir.StringType {
		return nil, fmt.Errorf("b64enc only applies to strings, got %s", child.Type)
	}
	return &b64encOp{op: op{name: s.name, child: child}}, nil
}

type b64encOp struct {
	op
}

func (p b64encOp) Eval(doc *ir.Node, env Env, ef EvalFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("b64enc on %s which has tag %q\n", doc.Path(), doc.Tag)
	}
	if err := ExpandEnv(doc, env); err != nil {
		return nil, err
	}
	if doc.Type != ir.StringType {
		return nil, fmt.Errorf("b64enc only applies to strings, got %s after evaluating", doc.Type)
	}
	enc := base64.RawStdEncoding.EncodeToString([]byte(doc.String))
	return ir.FromString(enc), nil
}
