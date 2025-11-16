package mergeop

import (
	"github.com/tony-format/tony/ir"
	"github.com/tony-format/tony/libdiff"
)

type Op interface {
	Match(doc *ir.Node, f MatchFunc) (bool, error)
	Patch(doc *ir.Node, mf MatchFunc, pf PatchFunc, df libdiff.DiffFunc) (*ir.Node, error)
	String() string
}

type op struct {
	name  Name
	child *ir.Node
}

func (o op) String() string {
	return o.name.String()
}

func (o op) IsMatch() bool {
	return o.name.IsMatch()
}

func (o op) IsPatch() bool {
	return o.name.IsPatch()
}
