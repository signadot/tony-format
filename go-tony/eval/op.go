package eval

import "github.com/signadot/tony-format/go-tony/ir"

type Env map[string]any

type Op interface {
	Eval(doc *ir.Node, env Env, f EvalFunc) (*ir.Node, error)
	String() string
}

type EvalFunc func(node *ir.Node, env Env) (*ir.Node, error)

type op struct {
	name  name
	child *ir.Node
}

func (o op) String() string {
	return string(o.name)
}
