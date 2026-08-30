package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var insertSym = &insertSymbol{patchName: insertName}

func Insert() Symbol {
	return insertSym
}

const (
	insertName patchName = "insert"
)

type insertSymbol struct {
	patchName
}

func (s insertSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("%s op has 1 or no args, got %d", s, len(args))
	}
	var tag *string
	if len(args) == 1 {
		tag = &args[0]
		if err := ir.CheckTag(*tag); err != nil {
			return nil, err
		}
	}
	return &insertOp{tag: tag, patchOp: patchOp{op: op{name: s.patchName, child: child}}}, nil
}

type insertOp struct {
	patchOp
	tag *string
}

// Patch answers with the value this carries, whatever it is applied to.
//
// The child is still applied as a PATCH rather than appended as data -- it can hold
// operations, and !insert(t) in particular means "insert this, tagged !t" -- but it
// is applied against ABSENCE. Applying it against the document merged the two, so
// !insert {b: 2} over {c: 9} answered {b: 2, c: 9}: the result depended on what was
// there, which is the one thing this operation promises it does not.
//
// Invisible where a diff emits it, because MakeDiff only emits !insert when there was
// nothing there and merging with nothing is replacing. It is a store that reaches the
// other case: a scope keeps the delta and baseline moves underneath it, so the value
// that was absent at the write is present at the read, and the scope's own view
// changes without anyone writing to it (5k4a6drqh12ksnsaj5n0).
//
// api.storableTags has always described this operation correctly -- "adds a value;
// the value is what results" -- and rested the storage vocabulary on it.
func (n insertOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("insert op called on %s\n", doc.Path())
	}
	res, err := pf(ir.Null(), n.child, ctx)
	if err != nil {
		return nil, err
	}
	// Nothing came back, so there is nothing to tag: no node is what a patch
	// which deleted the value answers with, and it is what this has to answer
	// with too.
	if res == nil {
		return nil, nil
	}
	if n.tag != nil {
		// the argument is a tag label, a node's Tag is the "!" and the label
		res = res.WithTag("!" + *n.tag)
	}
	return res, nil
}
