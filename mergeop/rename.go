package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/tony/debug"
	"github.com/signadot/tony-format/tony/ir"
	"github.com/signadot/tony-format/tony/libdiff"
)

var renameSym = &renameSymbol{patchName: renameName}

func Rename() Symbol {
	return renameSym
}

const (
	renameName patchName = "rename"
)

type renameSymbol struct {
	patchName
}

type renaming struct {
	from, to string
}

func (s renameSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op expects no args, got %v", s, args)
	}
	if child.Type != ir.ArrayType {
		return nil, fmt.Errorf("rename must be applied to an array ")
	}
	renamings := make([]renaming, len(child.Values))
	for _, v := range child.Values {
		from := ir.Get(v, "from")
		if from == nil {
			return nil, fmt.Errorf("renaming missing from at %s", child.Path())
		}
		if from.Type != ir.StringType {
			return nil, fmt.Errorf("renaming from should be string at %s", from.Path())
		}
		to := ir.Get(v, "to")
		if to == nil {
			return nil, fmt.Errorf("renaming missing to at %s", child.Path())
		}
		if to.Type != ir.StringType {
			return nil, fmt.Errorf("renaming to should be string at %s", to.Path())
		}
		renamings = append(renamings, renaming{from: from.String, to: to.String})
	}
	return &renameOp{
		renamings: renamings,
		patchOp:   patchOp{op: op{name: s.patchName, child: child}},
	}, nil
}

type renameOp struct {
	patchOp
	renamings []renaming
}

func (p renameOp) Patch(doc *ir.Node, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("rename op patch on %s\n", doc.Path())
	}
	if doc.Type != ir.ObjectType {
		return nil, fmt.Errorf("cannot rename fields in non-object at %s of type %s", doc.Path(), doc.Type)
	}
	docMap := ir.ToMap(doc)
	for i := range p.renamings {
		renaming := &p.renamings[i]
		fromVal := docMap[renaming.from]
		if fromVal == nil {
			continue
		}
		docMap[renaming.to] = fromVal
	}

	return ir.FromMap(docMap).WithTag(doc.Tag), nil
}
