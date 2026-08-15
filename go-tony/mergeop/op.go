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

// ArgumentOperand is implemented by an operation whose child is an ARGUMENT
// rather than a value it installs -- !comment's positions, say.
//
// A patch node carries the presentation of what is written on it, and for an op
// that installs its child that presentation belongs to the result. For an op
// whose child is an argument it does not: the result is the document, which has
// presentation of its own.
type ArgumentOperand interface {
	ArgumentOperand()
}
