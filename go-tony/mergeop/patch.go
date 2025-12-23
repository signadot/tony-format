package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
)

// PatchFunc is the signature for recursive patching operations.
// The context carries schema definitions and behavioral options.
type PatchFunc func(doc, patch *ir.Node, ctx *OpContext) (*ir.Node, error)

type patchOp struct {
	op
}

func (p patchOp) Match(_ *ir.Node, _ *OpContext, _ MatchFunc) (bool, error) {
	return false, fmt.Errorf("cannot match with %s operation", p)
}

func (p patchOp) IsMatch() bool {
	return p.name.IsMatch()
}

func (p patchOp) IsPatch() bool {
	return p.name.IsPatch()
}
