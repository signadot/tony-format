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

func (a allOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("all op called on %s\n", doc.Path())
	}
	switch doc.Type {
	// A child patch that deletes reports it by returning nil -- the convention
	// throughout -- and an element or field it deleted is one that is GONE, not
	// one that is nil. Keeping the nil put it in the slice for ir.FromSlice to
	// dereference, so `!all.if {..., else: !delete null}` over a list crashed.
	// Patching every child of a container does not change what the container IS:
	// it keeps its tag -- !key(f) above all, without which a keyed list comes back
	// unkeyed -- and its keys as written, a merge key included. Rebuilding it from a
	// string-keyed map lost both (4ynqp7wqh12krg32msn0 item 12).
	case ir.ObjectType:
		dst := make([]ir.KeyVal, 0, len(doc.Fields))
		for i := range doc.Fields {
			patch := a.child.Clone()
			patched, err := pf(doc.Values[i], patch, ctx)
			if err != nil {
				return nil, err
			}
			if patched == nil {
				continue
			}
			dst = append(dst, ir.KeyVal{Key: doc.Fields[i].Clone(), Val: patched})
		}
		return ir.FromKeyVals(dst).WithTag(doc.Tag), nil
	case ir.ArrayType:
		dst := make([]*ir.Node, 0, len(doc.Values))
		for _, docChild := range doc.Values {
			patch := a.child.Clone()
			patched, err := pf(docChild, patch, ctx)
			if err != nil {
				return nil, err
			}
			if patched == nil {
				continue
			}
			dst = append(dst, patched)
		}
		return ir.FromSlice(dst).WithTag(doc.Tag), nil
	default:
		return pf(doc, a.child, ctx)
	}
}

func (a allOp) Match(doc *ir.Node, ctx *OpContext, mf MatchFunc) (bool, error) {
	switch doc.Type {
	case ir.ObjectType:
		for i := range doc.Fields {
			docChild := doc.Values[i]
			subMatch, err := mf(docChild, a.child, ctx)
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
			subMatch, err := mf(docChild, a.child, ctx)
			if err != nil {
				return false, err
			}
			if !subMatch {
				return false, nil
			}
		}
		return true, nil
	default:
		return mf(doc, a.child, ctx)
	}
}
