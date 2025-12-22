package mergeop

import (
	"fmt"
	"path/filepath"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
)

var globSym = &globSymbol{matchName: globName}

func Glob() Symbol {
	return globSym
}

const (
	globName matchName = "glob"
)

type globSymbol struct {
	matchName
}

func (s globSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("glob op has no args, got %v", args)
	}
	return &globOp{matchOp: matchOp{op: op{name: s.matchName, child: child}}}, nil
}

type globOp struct {
	matchOp
}

func (g globOp) Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("glob op called on %s\n", doc.Path())
	}
	if g.child.Type != ir.StringType {
		return false, fmt.Errorf("cannot glob non-string")
	}
	if doc.Type != g.child.Type {
		return false, nil
	}
	m, err := filepath.Match(g.child.String, doc.String)
	if err != nil {
		return false, err
	}
	return m, nil
}
