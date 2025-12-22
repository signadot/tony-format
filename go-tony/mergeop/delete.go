package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var deleteSym = &deleteSymbol{deleteName}

func Delete() Symbol {
	return deleteSym
}

const (
	deleteName patchName = "delete"
)

type deleteSymbol struct {
	patchName
}

func (s deleteSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("%s op has 1 or no args, got %d", s, len(args))
	}
	var tag *string
	if len(args) == 1 {
		tag = &args[0]
		if err := ir.CheckTag(*tag); err != nil {
			return nil, err
		}
	}
	return &deleteOp{tag: tag, patchOp: patchOp{op: op{name: s.patchName, child: child}}}, nil
}

type deleteOp struct {
	patchOp
	tag *string
}

func (n deleteOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("delete op called on %s\n", doc.Path())
	}
	if n.tag != nil {
		if doc.Tag != *n.tag {
			return nil, fmt.Errorf("doc tag %s at %s didn't match %s", doc.Tag, doc.Path(), *n.tag)
		}
	}
	return nil, nil
}
