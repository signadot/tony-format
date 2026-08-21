package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var addTagSym = &addTagSymbol{patchName: addTagName}

func AddTag() Symbol {
	return addTagSym
}

const (
	addTagName patchName = "addtag"
)

type addTagSymbol struct {
	patchName
}

func (s addTagSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s op expects 1 args, got %d", s, len(args))
	}
	return &addTagOp{tag: args[0], patchOp: patchOp{op: op{name: s.patchName, child: child}}}, nil
}

type addTagOp struct {
	patchOp
	tag string
}

func (p addTagOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("addtag op patch on %s\n", doc.Path())
	}
	res, err := patchUnderTagDiff(doc, p.child, ctx, pf)
	if err != nil || res == nil {
		return nil, err
	}
	return res.WithTag("!" + p.tag), nil
}

// patchUnderTagDiff applies whatever a tag diff decorates.  A diff says a tag
// changed by composing !addtag, !rmtag or !retag over the diff of the value,
// which is a bare null when only the tag changed and the value's own diff when
// both did -- and dropping that would silently discard every change beneath it.
//
// It answers with no node when the diff beneath it deleted the value, which its
// three callers pass on: there is no node left to state a tag of, and writing
// one onto the nil was a crash apiece.
func patchUnderTagDiff(doc, child *ir.Node, ctx *OpContext, pf PatchFunc) (*ir.Node, error) {
	if child.Type == ir.NullType && child.Tag == "" {
		return doc.Clone(), nil
	}
	return pf(doc, child, ctx)
}
