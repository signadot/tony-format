package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var nullifySym = &nullifySymbol{patchName: nullifyName}

func Nullify() Symbol {
	return nullifySym
}

const (
	nullifyName patchName = "nullify"
)

type nullifySymbol struct {
	patchName
}

func (s nullifySymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op has no args, got %v", s, args)
	}
	return &nullifyOp{patchOp: patchOp{op: op{name: s.patchName, child: child}}}, nil
}

type nullifyOp struct {
	patchOp
}

func (n nullifyOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("nullify op patch on %s\n", doc.Path())
	}
	// A new null wearing the document's tag and line comment -- not the document
	// rewritten. A patch never mutates what it is given (field.go): rewriting it made a
	// store's base and its next state one object, so their difference was nil, the
	// operation was stored as sent, and every watcher stepping by it saw no change and
	// dropped the commit (fk1vg9sxh12ksyxxmdn0).
	res := &ir.Node{Type: ir.NullType, Tag: doc.Tag}
	if doc.Comment != nil {
		res.Comment = doc.Comment.Clone()
		res.Comment.Parent = res
	}
	return res, nil
}
