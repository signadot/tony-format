package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var retagSym = &retagSymbol{patchName: retagName}

func Retag() Symbol {
	return retagSym
}

const (
	retagName patchName = "retag"
)

type retagSymbol struct {
	patchName
}

func (s retagSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s op expects 2 args, got %d", s, len(args))
	}
	return &retagOp{from: args[0], to: args[1], patchOp: patchOp{op: op{name: s.patchName, child: child}}}, nil
}

type retagOp struct {
	patchOp
	from, to string
}

func (p retagOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("retag op patch on %s\n", doc.Path())
	}
	if doc.Tag != "!"+p.from {
		return nil, fmt.Errorf("doc tag %q doesn't match %q", doc.Tag, p.from)
	}
	return doc.Clone().WithTag("!" + p.to), nil
}
