package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

var atSym = &atSymbol{matchName: atName}

// At is the !at(kpath) operator: the match walks down the path and applies the
// pattern it holds to the node it lands on.
//
//	!at(spec.replicas).irtype 0
//
// matches a document whose spec.replicas is an integer, no matter what else it
// holds.  A path which names nothing does not match: !at(a.b) 3 asks for an a.b,
// so a document without one fails rather than matching vacuously -- the same
// reading !has-path gives a missing path.  A wildcard path (.* [*] {*}) names
// every node it reaches and all of them have to match, as every field an object
// pattern names has to match.
//
// The path is a kpath, all of it, keyed segments included: !at(resources(joe).x)
// reaches into the element keyed joe of a list the document tags !key(name).  A
// key names nothing in a list which is not keyed -- the tag is what says which
// field the key is -- so that is a mismatch, not an error.
//
// Composition reaches either side of the walk, and the two are different
// questions.  !not.at(a.b) 3 negates the whole thing -- it holds when there is
// no a.b as much as when a.b is 4 -- while !at(a.b).not 3 asks for an a.b which
// is something other than 3.
func At() Symbol {
	return atSym
}

const (
	atName matchName = "at"
)

type atSymbol struct {
	matchName
}

func (s atSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s op expects 1 arg (kpath), got %v", s, args)
	}
	// the walk reparses the path, but a pattern which cannot be read is a
	// defect in the pattern, and saying so where it is built beats reporting it
	// as a mismatch at every node the match visits
	if _, err := kpath.Parse(args[0]); err != nil {
		return nil, fmt.Errorf("%s(%s): %w", s, args[0], err)
	}
	return &atOp{
		path:    args[0],
		matchOp: matchOp{op: op{name: s.matchName, child: child}},
	}, nil
}

type atOp struct {
	matchOp
	path string
}

func (a atOp) Match(doc *ir.Node, ctx *OpContext, mf MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("at(%s) op match on %s\n", a.path, doc.Path())
	}
	// the nodes keep their parents, so a pattern which fails below is explained
	// at its place in the document rather than at the !at
	nodes, err := doc.ListKPath(nil, a.path)
	if err != nil {
		return false, err
	}
	if len(nodes) == 0 {
		return false, nil
	}
	for _, node := range nodes {
		matched, err := mf(node, a.child, ctx)
		if err != nil {
			return false, err
		}
		if !matched {
			return false, nil
		}
	}
	return true, nil
}
