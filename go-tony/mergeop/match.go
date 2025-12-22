package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

// MatchFunc is the signature for recursive matching operations.
// The context carries schema definitions and behavioral options.
type MatchFunc func(doc, pattern *ir.Node, ctx *OpContext) (bool, error)

type matchOp struct {
	op
}

func (m matchOp) Patch(_ *ir.Node, _ *OpContext, _ MatchFunc, _ PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	return nil, fmt.Errorf("cannot patch with %s operation", m)
}

func (m matchOp) IsMatch() bool {
	return m.name.IsMatch()
}

func (m matchOp) IsPatch() bool {
	return m.name.IsPatch()
}
