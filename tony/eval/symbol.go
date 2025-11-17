package eval

import "github.com/signadot/tony-format/tony/ir"

type Symbol interface {
	String() string
	Instance(child *ir.Node, args []string) (Op, error)
}

type name string

func (s name) String() string {
	return string(s)
}
