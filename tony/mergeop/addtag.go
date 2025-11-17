package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/tony/debug"
	"github.com/signadot/tony-format/tony/ir"
	"github.com/signadot/tony-format/tony/libdiff"
)

var addTagSym = &addTagSymbol{patchName: addTagName}

func AddTag() Symbol {
	return addTagSym
}

const (
	addTagName patchName = "addtag"
)

type addTagSymbol struct {
	patchName
}

func (s addTagSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s op expects 1 args, got %d", s, len(args))
	}
	return &addTagOp{tag: args[0], patchOp: patchOp{op: op{name: s.patchName, child: child}}}, nil
}

type addTagOp struct {
	patchOp
	tag string
}

func (p addTagOp) Patch(doc *ir.Node, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("addtag op patch on %s\n", doc.Path())
	}
	return doc.WithTag("!" + p.tag), nil
}
