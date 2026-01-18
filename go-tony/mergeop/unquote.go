package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
	"github.com/signadot/tony-format/go-tony/parse"
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

func (p unquoteOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, df libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("unquote op patch on %s\n", doc.Path())
	}
	uq, err := UnquoteY(doc)
	if err != nil {
		return nil, err
	}
	return uq, nil
}

func UnquoteY(node *ir.Node) (*ir.Node, error) {
	if node.Type != ir.StringType {
		return nil, fmt.Errorf("unquote requires a string, got %s", node.Type)
	}
	res, err := parse.Parse([]byte(node.String))
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, fmt.Errorf("unquote: empty input")
	}
	return res, nil
}
