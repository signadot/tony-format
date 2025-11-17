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

func (g fieldOp) Match(doc *ir.Node, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("match field op called on %s\n", doc.Path())
	}
	if g.from != nil {
		return false, fmt.Errorf("match field with patch field form !field(from)")
	}
	dummyNode := ir.FromString(doc.ParentField)
	return f(dummyNode, g.child)
}

func (g fieldOp) Patch(doc *ir.Node, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("patch field op called on %s\n", doc.Path())
	}
	if doc.Type != ir.ObjectType {
		return nil, fmt.Errorf("cannot patch field of non-object (in %s) at %s", doc.Type, doc.Path())
	}
	if g.from == nil {
		return nil, fmt.Errorf("field op didn't specify from, to")
	}
	newField := ir.FromString(*g.to)
	newField.Parent = doc
	newField.ParentField = newField.String
	for i, f := range doc.Fields {
		if f.String == *g.from {
			doc.Fields[i] = newField
			newField.ParentIndex = i
		}
	}
	if g.child.Tag != "" {
		return pf(doc, g.child)
	}
	return doc, nil
}
