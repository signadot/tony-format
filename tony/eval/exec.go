package eval

import (
	"bytes"
	"fmt"

	"github.com/signadot/tony-format/tony/debug"
	"github.com/signadot/tony-format/tony/ir"

	"os/exec"
)

var execSym = &execSymbol{name: execName}

func Exec() Symbol {
	return execSym
}

const (
	execName name = "exec"
)

type execSymbol struct {
	name
}

func (s execSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op expects no args, got %v", s, args)
	}
	if child.Type != ir.StringType {
		return nil, fmt.Errorf("exec only applies to strings, got %s", child.Type)
	}
	return &execOp{op: op{name: s.name, child: child}}, nil
}

type execOp struct {
	op
}

func (p execOp) Eval(doc *ir.Node, env Env, ef EvalFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("exec on %s\n", doc.Path())
	}
	if err := ExpandEnv(doc, env); err != nil {
		return nil, err
	}
	if doc.Type != ir.StringType {
		return nil, fmt.Errorf("exec only applies to strings, got %s after expanding env", doc.Type)
	}
	cmd := exec.Command("sh", "-c", doc.String)
	buf := bytes.NewBuffer(nil)
	cmd.Stdout = buf
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return ir.FromString(buf.String()), nil
}
