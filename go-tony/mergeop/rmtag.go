package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var rmTagSym = &rmTagSymbol{patchName: rmTagName}

func RemoveTag() Symbol {
	return rmTagSym
}

const (
	rmTagName patchName = "rmtag"
)

type rmTagSymbol struct {
	patchName
}

func (s rmTagSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s op expects 1 args, got %d", s, len(args))
	}
	return &rmTagOp{tag: args[0], patchOp: patchOp{op: op{name: s.patchName, child: child}}}, nil
}

type rmTagOp struct {
	patchOp
	tag string
}

func (p rmTagOp) Patch(doc *ir.Node, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("rmtag op patch on %s\n", doc.Path())
	}
	return doc.WithTag(p.tag), nil
}
