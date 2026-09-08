package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var fieldSym = &fieldSymbol{name: fieldName}

func Field() Symbol {
	return fieldSym
}

const (
	fieldName name = "field"
)

type fieldSymbol struct {
	name
}

func (s fieldSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 && len(args) != 2 {
		return nil, fmt.Errorf("field op has no or 2 args, got %v", args)
	}
	res := &fieldOp{op: op{name: s.name, child: child}}
	if len(args) == 2 {
		res.from = &args[0]
		res.to = &args[1]
	}
	return res, nil
}

type fieldOp struct {
	op
	from, to *string
}

func (g fieldOp) Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("match field op called on %s\n", doc.Path())
	}
	if g.from != nil {
		return false, fmt.Errorf("match field with patch field form !field(from)")
	}
	dummyNode := ir.FromString(doc.ParentField)
	return f(dummyNode, g.child, ctx)
}

func (g fieldOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("patch field op called on %s\n", doc.Path())
	}
	if doc.Type != ir.ObjectType {
		return nil, fmt.Errorf("cannot patch field of non-object (in %s) at %s", doc.Type, doc.Path())
	}
	if g.from == nil {
		return nil, fmt.Errorf("field op didn't specify from, to")
	}
	// A new object, with the field renamed, sharing the values -- as every merge in this
	// package builds its result. The document is not touched: a patch never mutates what
	// it is given, and a store that keeps a document and steps it by each patch relies
	// on that. Renaming in place made the base and the result one object, so a store's
	// diff of the two saw no change, kept the relative operation as written, and
	// rewrote the kept document under every reader sharing its subtrees.
	kvs := make([]ir.KeyVal, 0, len(doc.Fields))
	for i, f := range doc.Fields {
		if i >= len(doc.Values) {
			break
		}
		key := f
		if f.String == *g.from {
			key = ir.FromString(*g.to)
		}
		kvs = append(kvs, ir.KeyVal{Key: key, Val: doc.Values[i]})
	}
	out := ir.FromKeyVals(kvs).WithTag(doc.Tag)
	if g.child.Tag != "" {
		return pf(out, g.child, ctx)
	}
	return out, nil
}
