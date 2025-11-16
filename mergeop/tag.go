package mergeop

import (
	"fmt"

	"github.com/tony-format/tony/debug"
	"github.com/tony-format/tony/ir"
)

var tagSym = &tagSymbol{matchName: tagName}

func Tag() Symbol {
	return tagSym
}

const (
	tagName matchName = "tag"
)

type tagSymbol struct {
	matchName
}

func (s tagSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("tag op has no args, got %v", args)
	}
	return &tagOp{matchOp: matchOp{op: op{name: s.matchName, child: child}}}, nil
}

type tagOp struct {
	matchOp
}

func (g tagOp) Match(doc *ir.Node, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("tag op called on %s with tag %q\n", doc.Path(), doc.Tag)
	}
	tag := doc.Tag
	if tag != "" {
		tag = tag[1:] // chop !
	}
	dummyNode := ir.FromString(tag)
	return f(dummyNode, g.child)
}
