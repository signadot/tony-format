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
	if g.from == nil {
		return nil, fmt.Errorf("field op didn't specify from, to")
	}
	// A new object, with the field renamed, sharing the values -- as every merge in this
	// package builds its result. The document is not touched: a patch never mutates what
	// it is given, and a store that keeps a document and steps it by each patch relies
	// on that. Renaming in place made the base and the result one object, so a store's
	// diff of the two saw no change, kept the relative operation as written, and
	// rewrote the kept document under every reader sharing its subtrees.
	//
	// It is !rename's renaming, refusals and all. It had its own, which refused nothing:
	// a to the object already held answered with two fields of one name -- which a scope
	// stored, a shape the IR cannot hold -- and at baseline the renamed value was lost
	// from the stored delta; a from the object did not have renamed nothing, and the write
	// was told it had been made (e5wt4fhxh12ksz5xmdn0).
	out, err := renameFields(doc, g.name, []renaming{{from: *g.from, to: *g.to}})
	if err != nil {
		return nil, err
	}
	if g.child.Tag != "" {
		return pf(out, g.child, ctx)
	}
	return out, nil
}
