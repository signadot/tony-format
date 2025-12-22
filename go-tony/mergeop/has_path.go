package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
)

var hasPathSym = &hasPathSymbol{matchName: hasPathName}

func HasPath() Symbol {
	return hasPathSym
}

const (
	hasPathName matchName = "has-path"
)

type hasPathSymbol struct {
	matchName
}

func (s hasPathSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("has-path op has no args, got %v", args)
	}
	return &hasPathOp{matchOp: matchOp{op: op{name: s.matchName, child: child}}}, nil
}

type hasPathOp struct {
	matchOp
}

func (h hasPathOp) Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("has-path op called on %s\n", doc.Path())
	}
	if h.child.Type != ir.StringType {
		return false, fmt.Errorf("has-path op expects string path, got %s", h.child.Type)
	}
	path := h.child.String
	node, err := doc.GetKPath(path)
	if err != nil {
		// Invalid path or error
		return false, nil
	}
	if node == nil {
		// Path doesn't exist
		return false, nil
	}
	// Path exists
	return true, nil
}
