package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
)

type PatchFunc func(*ir.Node, *ir.Node) (*ir.Node, error)

type patchOp struct {
	op
}

func (p patchOp) Match(*ir.Node, MatchFunc) (bool, error) {
	return false, fmt.Errorf("cannot match with %s operation", p)
}

func (p patchOp) IsMatch() bool {
	return p.name.IsMatch()
}

func (p patchOp) IsPatch() bool {
	return p.name.IsPatch()
}
