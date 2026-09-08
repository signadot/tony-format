package api

import (
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/mergeop"
)

// ProjectDelta answers what a document-rooted delta says at or below kp, re-rooted at kp:
// nil when it says nothing about kp, and ok false when it cannot be seen from kp -- an
// operator above kp states the whole value there, or a value kp descends into is not a
// container the next segment can step into -- in which case depth is how many of kp's
// segments were descended before the block, which names the ancestor whose value is the
// answer. It is the ONE ROOTING RULE (one_delta_shape.md): a read projects a stored entry
// onto its path this way, a watch projects the same entry onto the path it watches the
// same way, and docd projects a composed delta onto a client's path the same way, so the
// delta a client receives for a path is a delta AT that path, whatever made it.
func ProjectDelta(delta *ir.Node, kp string) (at *ir.Node, depth int, ok bool) {
	if delta == nil {
		return nil, 0, true
	}
	segs := kpath.SplitAll(kp)
	n := delta
	for depth = range segs {
		n = ir.Uncomment(n)
		if n == nil {
			return nil, depth, true
		}
		if HasOperator(n.Tag) {
			return nil, depth, false
		}
		if n.Type != ir.ObjectType {
			// A scalar or a list where kp descends: the write replaces the node kp is
			// inside, which is a statement about the ancestor and not about kp.
			return nil, depth, false
		}
		name, isField := kpath.SegmentFieldName(segs[depth])
		if !isField {
			return nil, depth, false // an index: the array is the unit
		}
		next := ir.Get(n, name)
		if next == nil {
			return nil, depth, true // the delta does not reach kp
		}
		n = next
	}
	return n, len(segs), true
}

// HasOperator reports whether a tag chain names a merge operation, which is what makes a
// node's subtree unable to speak for it. Presentation and data tags are not operations:
// they travel with the value and say nothing about how it merges.
func HasOperator(tag string) bool {
	for t := tag; t != ""; {
		head, _, rest := ir.TagArgs(t)
		if head == "" {
			return false
		}
		if mergeop.Lookup(head[1:]) != nil {
			return true
		}
		if rest == t {
			return false
		}
		t = rest
	}
	return false
}
