package eval

import (
	"fmt"
	"os"
	"strings"

	"github.com/signadot/tony-format/tony/debug"
	"github.com/signadot/tony-format/tony/ir"
)

var osenvSym = &osenvSymbol{name: osenvName}

func OSEnv() Symbol {
	return osenvSym
}

const (
	osenvName name = "osenv"
)

type osenvSymbol struct {
	name
}

func (s osenvSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op expects no args, got %v", s, args)
	}
	if child.Type != ir.StringType {
		return nil, fmt.Errorf("osenv only applies to strings, got %s", child.Type)
	}
	return &osenvOp{op: op{name: s.name, child: child}}, nil
}

type osenvOp struct {
	op
}

func (p osenvOp) Eval(doc *ir.Node, env Env, ef EvalFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("osenv on %s\n", doc.Path())
	}
	if err := ExpandEnv(doc, env); err != nil {
		return nil, err
	}
	if doc.Type != ir.StringType {
		return nil, fmt.Errorf("osenv only applies to strings, got %s after expanding env", doc.Type)
	}
	return ir.FromString(os.Getenv(strings.TrimSpace(doc.String))), nil
}
