package mergeop

import (
	"errors"
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var ifSym = &ifSymbol{patchName: ifName}

func If() Symbol {
	return ifSym
}

const (
	ifName patchName = "if"
)

type ifSymbol struct {
	patchName
	If   *ir.Node
	Then *ir.Node
	Else *ir.Node
}

func (s ifSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op expects no args, got %v", s, args)
	}
	if child.Type != ir.ObjectType {
		return nil, errors.New("if must be an object")
	}
	yIf := ir.Get(child, "if")
	if yIf == nil {
		return nil, errors.New("must have if match")
	}
	yThen := ir.Get(child, "then")
	yElse := ir.Get(child, "else")
	if yThen == nil && yElse == nil {
		return nil, errors.New("if must have then or else or both")
	}

	op := &ifOp{
		patchOp: patchOp{op: op{name: s.patchName, child: child}},
		If:      yIf,
		Then:    yThen,
		Else:    yElse,
	}
	return op, nil
}

type ifOp struct {
	patchOp
	If   *ir.Node
	Then *ir.Node
	Else *ir.Node
}

func (a ifOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("if op called on %s\n", doc.Path())
	}
	m, err := mf(doc, a.If, ctx)
	if err != nil {
		return nil, err
	}
	if m {
		return pf(doc, a.Then, ctx)
	}
	return pf(doc, a.Else, ctx)
}
