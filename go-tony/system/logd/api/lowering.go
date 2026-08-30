package api

import (
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
	"github.com/signadot/tony-format/go-tony/mergeop"
)

// firstRelativeOp answers the first operation in a tag chain that a store may not
// keep as it arrived, or "" when there is none.
//
// A tag composes with '.', so !insert.retag(x,y) names TWO operations and the second
// binds as much as the first. mergeop.SplitChild answers only the head, so asking it
// alone said "insert, storable" and stopped: a relative operation written behind an
// absolute one passed the vocabulary check entirely. mergeop.FindUnsafe has always
// read the whole chain; this is that walk asked about storability.
//
// A tag that names no registered operation is neither storable nor not: data tags and
// presentation tags travel in the same chain and are not operations at all.
//
// The walk ENDS at !raw, because that is what the escape says: nothing after it is
// interpreted, so nothing after it is an operation to hold to this vocabulary -- it is
// data that happens to be shaped like one. Reading past it refused the one escape that
// lets a document holding operators be stored at all, and refused it in the form an
// escaping writer actually produces: escaping a LEAF has nowhere to put the label but the
// node's own tag, so !irtype escaped is !raw.irtype (fch8ptynh12ksfvvjdn0).
//
// Where the escape sits is what decides, which is why this ends the walk rather than
// scanning the chain for a raw anywhere in it. A chain reads left to right, so an
// operation BEFORE the escape binds and is applied to the raw data -- !strdiff.raw is a
// strdiff -- while everything after it is the data. The two are the same labels in the
// two orders and must not answer alike.
func firstRelativeOp(tag string) string {
	for tag != "" {
		head, _, rest := ir.TagArgs(tag)
		head = strings.TrimPrefix(head, "!")
		if "!"+head == libdiff.RawTag {
			return ""
		}
		if mergeop.Lookup(head) != nil && !IsStorableTag(head) {
			return head
		}
		tag = rest
	}
	return ""
}

// NeedsLowering answers whether a patch has to be lowered before it can be stored,
// and names the first operation that says so.
//
// The vocabulary above is the ABSOLUTE operations -- those whose result is a
// statement of what the value is, rather than of how it relates to what was there.
// A patch built only from those is already its own delta: replaying it against a
// base that has moved gives what it gave at the write, so the store keeps it as it
// arrived and never has to read the state it was written against.
//
// Anything else has to be applied and the RESULT diffed. That is the whole cost of
// lowering -- a read of the current state on the write path -- and this is what
// keeps it off the ordinary write. A plain data merge carries no operation at all,
// and the operations logd injects for itself (!logd-key, !logd-auto-id, and the
// !key and !insert they become) are all absolute, so the common write answers
// false here and pays nothing.
//
// It does NOT answer for mergeop.Unsafe: a !pipe is refused rather than lowered,
// because lowering it would mean running an arbitrary system call inside commitMu.
// tx asks that question first, and this one never sees such a patch.
//
// The walk mirrors validateForStorage's, and has to: an operation this misses is
// one stored unlowered, which is the defect the vocabulary exists to prevent.
func NeedsLowering(n *ir.Node) (op string, yes bool) {
	if n == nil {
		return "", false
	}
	if o := firstRelativeOp(n.Tag); o != "" {
		return "!" + o, true
	}
	// Beneath !raw nothing is interpreted, so nothing beneath is an operation --
	// it is data shaped like one, which is what a stored rule, charter or patch
	// is (6225etzfh12kr955fxn0). The node's own chain is read above, so
	// !strdiff.raw still answers for the strdiff.
	if ir.TagHas(n.Tag, "!raw") {
		return "", false
	}
	// A head comment wraps the value it precedes and is not a kind of container,
	// so a walk that stopped at the wrapper would miss everything under it
	// (3cdjz00jh12krns4g1n0).
	n = ir.Uncomment(n)
	if n == nil {
		return "", false
	}
	switch n.Type {
	case ir.ObjectType:
		for i := range n.Fields {
			if i >= len(n.Values) {
				break
			}
			if op, yes := NeedsLowering(n.Values[i]); yes {
				return op, true
			}
		}
	case ir.ArrayType:
		for _, v := range n.Values {
			if op, yes := NeedsLowering(v); yes {
				return op, true
			}
		}
	}
	return "", false
}
