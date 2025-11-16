package mergeop

import (
	"fmt"

	"github.com/tony-format/tony/debug"
	"github.com/tony-format/tony/ir"
	"github.com/tony-format/tony/libdiff"
)

var insertSym = &insertSymbol{patchName: insertName}

func Insert() Symbol {
	return insertSym
}

const (
	insertName patchName = "insert"
)

type insertSymbol struct {
	patchName
}

func (s insertSymbol) Instance(child *ir.Node, args []string) (Op, error) {
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
	return &insertOp{tag: tag, patchOp: patchOp{op: op{name: s.patchName, child: child}}}, nil
}

type insertOp struct {
	patchOp
	tag *string
}

func (n insertOp) Patch(doc *ir.Node, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("insert op called on %s\n", doc.Path())
	}
	res, err := pf(doc, n.child)
	if err != nil {
		return nil, err
	}
	if n.tag != nil {
		res = res.WithTag(*n.tag)
	}
	return res, nil
}
