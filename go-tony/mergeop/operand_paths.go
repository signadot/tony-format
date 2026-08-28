package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
)

// Operand is one part of an operation's operand that is a DOCUMENT value: a node
// whose contents sit somewhere in the document the operation is applied to.
type Operand struct {
	Node *ir.Node
	// Suffix is appended to the operation's own path to reach Node. Empty means
	// Node sits exactly where the operation does, which is the common case: what
	// an !insert carries becomes the value at that path, not at a path below it.
	Suffix string
}

// OperandPaths answers which parts of an operation's operand are document values,
// and where each sits relative to the node carrying the operation.
//
// It exists because every walk over a patch has to know this and none of them did.
// They descended into an operand as if it were ordinary structure, so a write of
//
//	{a: !comment {head: ["# note"]}}
//
// indexed the paths a.head and a.head[0], which are not in the document at all. That
// is harmless while the operand holds only a comment's own lines -- nothing reads
// those paths -- and stops being harmless the moment an operand carries a value: a
// change at a.e recorded as a.child.e is a change a narrow read of a.e cannot find.
//
// ok is false when the node carries no operation, or one this has no answer for. The
// caller then walks it as it always did, so adding an operation to tony cannot
// silently change how a walk reads it -- it just does not get the benefit until it is
// named here.
//
// The answer is about PATHS, not about where operations may hide. A walk looking for
// operations (mergeop.FindUnsafe, api.NeedsLowering) wants every node beneath, stopping
// only at !raw; a walk assigning paths (the index, patchMayAffect, the overlay's
// annotation) wants this.
func OperandPaths(n *ir.Node) (ops []Operand, ok bool) {
	if n == nil {
		return nil, false
	}
	_, name, _, child, err := SplitChild(ir.Uncomment(n))
	if err != nil || name == "" || child == nil {
		return nil, false
	}
	switch name {
	case string(rawName):
		// The escape hides OPERATIONS, not paths. What it carries is data and the
		// data lands where the escape is, so its paths are document paths like any
		// other -- a stored rule at spec.rules is read at spec.rules. Answering
		// nothing here lost every path inside an escaped value.
		return []Operand{{Node: child, Suffix: ""}}, true

	case string(strDiffName):
		// An edit script over the string that was there. Its entries are counts
		// and runs, not paths.
		return nil, true

	case string(commentName):
		// The operand names comment POSITIONS -- head, line -- which are not
		// paths. See comment.go.
		return nil, true

	case string(keyedListName):
		// !key annotates the array rather than consuming it: the array IS the
		// operand, and its elements are keyed path segments the caller's own array
		// handling knows how to name. Stripping the tag to hand back the array
		// would take the keying with it, and the elements would be indexed by
		// position.
		return nil, false

	case string(insertName), string(deleteName), string(addTagName),
		string(rmTagName):
		// The operand is the value, and it sits where the operation sits. For a
		// !delete that value is what WENT AWAY, and recording its paths is how
		// the index knows the document no longer has them -- which is why the
		// payload under a delete is not dead weight.
		return []Operand{{Node: child, Suffix: ""}}, true

	case string(replaceName), string(retagName):
		// Checked operations: from: is what must still be there and to: is what
		// results. Both describe the same path.
		var out []Operand
		if to := ir.Get(child, "to"); to != nil {
			out = append(out, Operand{Node: to, Suffix: ""})
		}
		if from := ir.Get(child, "from"); from != nil {
			out = append(out, Operand{Node: from, Suffix: ""})
		}
		return out, true

	case string(arrayDiffName):
		// The one operand whose parts sit BELOW the operation: each field names
		// an index in the array that was there.
		c := ir.Uncomment(child)
		if c == nil || c.Type != ir.ObjectType {
			return nil, true
		}
		var out []Operand
		for i, f := range c.Fields {
			if i >= len(c.Values) || f.Type != ir.NumberType || f.Int64 == nil {
				continue
			}
			out = append(out, Operand{
				Node:   c.Values[i],
				Suffix: fmt.Sprintf("[%d]", *f.Int64),
			})
		}
		return out, true
	}
	return nil, false
}
