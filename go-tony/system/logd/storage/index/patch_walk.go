package index

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// PatchChild is a node a patch holds beneath another, at the kinded path the patch
// states about it.
type PatchChild struct {
	Path string
	Node *ir.Node
}

// PatchChildren answers what a patch's own STRUCTURE holds beneath n, each at the
// kinded path the patch states about it. An empty answer means the patch says
// something about `at` itself rather than about anything below it.
//
// This is the one reading of "where in the document does this part of the patch land",
// and there is one of it because every walk over a patch needs it and each answered it
// again: a field is a .field step, an integer-keyed object is a {sparse} step, an array
// is [i] unless it is keyed, and a keyed one is (key) read the way ir.ElemKey reads it,
// so a path recorded here is a path a reader can follow back. schema may be nil, and
// then only a !key tag the patch carries makes an array keyed.
//
// It does NOT look inside an operand. What an operand means is the operation's
// business -- descending into one records paths the document does not have, and worse,
// records a value the operand carries at a path below where it actually sits.
// mergeop.OperandPaths is the one place that knows which parts of which operand are
// document values and where each sits; a caller that wants them asks it. A caller that
// wants to stop at the operation -- because what the operation MEANT is about the node
// wearing it -- simply does not.
func PatchChildren(n *ir.Node, at string, schema *api.Schema) []PatchChild {
	if n == nil {
		return nil
	}
	switch n.Type {
	case ir.ObjectType:
		if len(n.Fields) == 0 {
			return nil
		}
		out := make([]PatchChild, 0, len(n.Fields))
		if n.Fields[0].Type == ir.NumberType {
			for i, f := range n.Fields {
				if f.Int64 == nil || i >= len(n.Values) {
					continue
				}
				out = append(out, PatchChild{
					Path: fmt.Sprintf("%s{%d}", at, *f.Int64),
					Node: n.Values[i],
				})
			}
			return out
		}
		for i := range n.Fields {
			if i >= len(n.Values) {
				break
			}
			out = append(out, PatchChild{
				Path: kpath.ChildField(at, n.Fields[i].String),
				Node: n.Values[i],
			})
		}
		return out

	case ir.ArrayType:
		// The schema is asked first, then the !key tag the patch carries. keyed is
		// tracked apart from keyField because a bare !key keys its elements by
		// themselves, which is an empty field and still a keyed list.
		keyField, keyed := "", false
		if schema != nil {
			keyField = schema.LookupKeyField(at)
			keyed = keyField != ""
		}
		if !keyed {
			keyField, keyed = n.KeyField()
		}
		out := make([]PatchChild, 0, len(n.Values))
		for i, v := range n.Values {
			if !keyed {
				out = append(out, PatchChild{
					Path: fmt.Sprintf("%s[%d]", at, i),
					Node: v,
				})
				continue
			}
			// default to "" for things which are not indexable this way.
			indexVal, _ := ir.ElemKey(v, keyField)
			out = append(out, PatchChild{
				Path: at + kpath.Key(indexVal).SegmentString(),
				Node: v,
			})
		}
		return out
	}
	return nil
}
