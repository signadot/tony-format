package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/tony/ir"
	"github.com/signadot/tony-format/tony/libdiff"
)

type MatchFunc func(*ir.Node, *ir.Node) (bool, error)

type matchOp struct {
	op
}

func (m matchOp) Patch(_ *ir.Node, _ MatchFunc, _ PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	return nil, fmt.Errorf("cannot patch with %s operation", m)
}

func (m matchOp) IsMatch() bool {
	return m.name.IsMatch()
}

func (m matchOp) IsPatch() bool {
	return m.name.IsPatch()
}
