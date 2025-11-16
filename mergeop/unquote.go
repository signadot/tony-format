package mergeop

import (
	"fmt"

	"github.com/tony-format/tony/debug"
	"github.com/tony-format/tony/ir"
	"github.com/tony-format/tony/libdiff"
	"github.com/tony-format/tony/parse"
)

var unquoteSym = &unquoteSymbol{patchName: unquoteName}

func Unquote() Symbol {
	return unquoteSym
}

const (
	unquoteName patchName = "unquote"
)

type unquoteSymbol struct {
	patchName
}

func (s unquoteSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op expects no args, got %v", s, args)
	}
	return &unquoteOp{patchOp: patchOp{op: op{name: s.patchName, child: child}}}, nil
}

type unquoteOp struct {
	patchOp
}

func (p unquoteOp) Patch(doc *ir.Node, mf MatchFunc, pf PatchFunc, df libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("unquote op patch on %s\n", doc.Path())
	}
	childPatched, err := pf(doc, p.child)
	if err != nil {
		return nil, err
	}
	uq, err := UnquoteY(childPatched)
	if err != nil {
		return nil, err
	}
	return uq, nil
}

func UnquoteY(node *ir.Node) (*ir.Node, error) {
	return parse.Parse([]byte(node.String))
}
