package mergeop

import (
	"github.com/signadot/tony-format/go-tony/ir"
)

// FindUnsafe reports the first operation in a patch which calls out to the system,
// so a caller can refuse the patch instead of discovering it while applying one.
//
// Unsafe(name) says which operations those are; this is the same question asked of
// a whole patch rather than of one tag. RejectUnsafe answers it at apply time,
// which is the last line and the wrong place to be told: a store which has already
// accepted the patch has to fail every read of it instead, and a caller which sent
// it has already been told the write succeeded.
//
// The walk stops at !raw, exactly as the applier does. Beneath !raw nothing is
// interpreted -- that is what the escape is for -- so an operator there is the text
// of one, not one, and a document which merely CONTAINS a patch is storable
// (6225etzfh12kr955fxn0). The node's own chain is still read: !pipe.raw is a pipe.
func FindUnsafe(n *ir.Node) (string, bool) {
	// A head comment wraps the value it precedes, and this asks what is IN the
	// patch. A comment is not a kind of container, so a walk which stopped at the
	// wrapper would miss everything under it.
	n = ir.Uncomment(n)
	if n == nil {
		return "", false
	}
	for tag := n.Tag; tag != ""; {
		head, _, rest := ir.TagArgs(tag)
		if len(head) > 0 && head[0] == '!' {
			head = head[1:]
		}
		if Unsafe(head) {
			return head, true
		}
		tag = rest
	}
	if ir.TagHas(n.Tag, "!raw") {
		return "", false
	}
	for _, v := range n.Values {
		if op, found := FindUnsafe(v); found {
			return op, true
		}
	}
	return "", false
}
