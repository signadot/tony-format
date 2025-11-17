package mergeop

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/signadot/tony-format/tony/debug"
	"github.com/signadot/tony-format/tony/ir"
	"github.com/signadot/tony-format/tony/libdiff"
)

var strDiffSym = &strDiffSymbol{patchName: strDiffName}

func StrDiff() Symbol {
	return strDiffSym
}

const (
	strDiffName patchName = "strdiff"
)

type strDiffSymbol struct {
	patchName
}

func (s strDiffSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s op expects 1 arg, got %v", s, args)
	}
	multiLine, err := strconv.ParseBool(args[0])
	if err != nil {
		return nil, fmt.Errorf("strdiff requires a boolean (multiline) argument: %w", err)
	}

	if child.Type != ir.ObjectType {
		return nil, errors.New("strdiff op needs an object")
	}
	return &strDiffOp{
		multiLine: multiLine,
		patchOp:   patchOp{op: op{name: s.patchName, child: child}},
	}, nil
}

type strDiffOp struct {
	patchOp
	multiLine bool
}

func (op strDiffOp) Patch(doc *ir.Node, mf MatchFunc, pf PatchFunc, df libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("patch op strdiff on %s\n", doc.Path())
	}

	if doc.Type != ir.StringType {
		return nil, fmt.Errorf("strdiff only applies to strings, got %s", doc.Type)
	}
	if op.multiLine {
		return libdiff.PatchStringMultiLine(doc, op.child)
	}
	return libdiff.PatchStringRunes(doc, op.child)
}
