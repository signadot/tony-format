package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/eval"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
	"github.com/signadot/tony-format/go-tony/parse"

	jsonpatch "github.com/evanphx/json-patch"
)

var jPatchSym = &jPatchSymbol{patchName: jPatchName}

func JSONPatch() Symbol {
	return jPatchSym
}

const (
	jPatchName patchName = "json-patch"
)

type jPatchSymbol struct {
	patchName
}

func (s jPatchSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op has no args, got %v", s, args)
	}
	d, err := eval.MarshalJSON(child)
	if err != nil {
		return nil, err
	}
	ops, err := jsonpatch.DecodePatch(d)
	if err != nil {
		return nil, err
	}
	return &jPatchOp{ops: ops, patchOp: patchOp{op: op{name: s.patchName, child: child}}}, nil
}

type jPatchOp struct {
	patchOp
	ops jsonpatch.Patch
}

// TODO make this native *y.Y to preserve comments
func (jp jPatchOp) Patch(doc *ir.Node, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("jsonpatch op called on %s\n", doc.Path())
	}
	d, err := eval.MarshalJSON(doc)
	if err != nil {
		return nil, err
	}
	jOut, err := jp.ops.Apply(d)
	if err != nil {
		return nil, err
	}
	yOut, err := parse.Parse(jOut)
	if err != nil {
		return nil, err
	}
	return yOut, nil
}
