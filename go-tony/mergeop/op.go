package mergeop

import (
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

type Op interface {
	Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error)
	Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, df libdiff.DiffFunc) (*ir.Node, error)
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
