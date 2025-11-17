package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var nullifySym = &nullifySymbol{patchName: nullifyName}

func Nullify() Symbol {
	return nullifySym
}

const (
	nullifyName patchName = "nullify"
)

type nullifySymbol struct {
	patchName
}

func (s nullifySymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op has no args, got %v", s, args)
	}
	return &nullifyOp{patchOp: patchOp{op: op{name: s.patchName, child: child}}}, nil
}

type nullifyOp struct {
	patchOp
}

func (n nullifyOp) Patch(doc *ir.Node, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("nullify op patch on %s\n", doc.Path())
	}
	doc.Type = ir.NullType
	doc.Fields = nil
	doc.Values = nil
	return doc, nil
}
