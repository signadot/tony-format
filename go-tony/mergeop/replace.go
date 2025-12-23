package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var replaceSym = &replaceSymbol{patchName: replaceName}

func Replace() Symbol {
	return replaceSym
}

const (
	replaceName patchName = "replace"
)

type replaceSymbol struct {
	patchName
}

func (s replaceSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op has no args, got %d", s, len(args))
	}
	if child.Type != ir.ObjectType {
		return nil, fmt.Errorf("replace expects object child at %s", child.Path())
	}
	from := ir.Get(child, "from")
	if from == nil {
		return nil, fmt.Errorf("replace expects from: at %s", child.Path())
	}
	to := ir.Get(child, "to")
	if to == nil {
		return nil, fmt.Errorf("replace expects to: at %s", child.Path())
	}
	return &replaceOp{from: from, to: to, patchOp: patchOp{op: op{name: s.patchName, child: child}}}, nil
}

type replaceOp struct {
	patchOp
	from *ir.Node
	to   *ir.Node
}

func (n replaceOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, df libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("replace op called on %s\n", doc.Path())
	}
	if x := df(doc, n.from); x != nil {
		return nil, fmt.Errorf("# node at %s differes from replacement from: at %s\n# diff:\n%s", doc.Path(), n.from.Path(), encode.MustString(x))
	}
	return n.to.Clone(), nil
}
