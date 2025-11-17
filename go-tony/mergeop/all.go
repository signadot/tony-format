package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var allSym = &allSymbol{name: allName}

func All() Symbol {
	return allSym
}

const (
	allName name = "all"
)

type allSymbol struct {
	name
}

func (s allSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op expects no args, got %v", s, args)
	}
	return &allOp{op: op{name: s.name, child: child}}, nil
}

type allOp struct {
	op
}

func (a allOp) Patch(doc *ir.Node, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("all op called on %s\n", doc.Path())
	}
	switch doc.Type {
	case ir.ObjectType:
		dst := make(map[string]*ir.Node, len(doc.Fields))
		for i := range doc.Fields {
			field := doc.Fields[i]
			patch := a.child.Clone()
			patched, err := pf(doc.Values[i], patch)
			if err != nil {
				return nil, err
			}
			dst[field.String] = patched
		}
		return ir.FromMap(dst), nil
	case ir.ArrayType:
		dst := make([]*ir.Node, len(doc.Values))
		for i, docChild := range doc.Values {
			patch := a.child.Clone()
			patched, err := pf(docChild, patch)
			if err != nil {
				return nil, err
			}
			dst[i] = patched
		}
		return ir.FromSlice(dst), nil
	default:
		return pf(doc, a.child)
	}
}

func (a allOp) Match(doc *ir.Node, mf MatchFunc) (bool, error) {
	switch doc.Type {
	case ir.ObjectType:
		for i := range doc.Fields {
			docChild := doc.Values[i]
			subMatch, err := mf(docChild, a.child)
			if err != nil {
				return false, err
			}
			if !subMatch {
				return false, nil
			}
		}
		return true, nil
	case ir.ArrayType:
		for _, docChild := range doc.Values {
			subMatch, err := mf(docChild, a.child)
			if err != nil {
				return false, err
			}
			if !subMatch {
				return false, nil
			}
		}
		return true, nil
	default:
		return mf(doc, a.child)
	}
}
