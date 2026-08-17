package storage

import (
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// AbsentSpineAt answers a document which resolves exactly as far as kp has ever been
// written, and no further -- when the index can say that kp has never been written at
// all. The second result is false when it cannot say that, and then the caller reads
// as it did before.
//
// It exists because the cheapest answer cost the most. A read at a path with nothing
// at it could not narrow -- there is nothing to narrow to -- so it fell back to the
// whole document, and a charter of ten rules over slices nobody has written yet paid
// ten full reads to be told "nothing here" ten times. Measured on a staging verse:
// eleven of twenty-five reads were absent paths, at 79ms to 269ms each
// (ap8ddvp2h12krd43gdn0).
//
// The index is a trie of every path ever written, since a patch is indexed at every
// path inside it. So a path with no node in it has never been written, and a document
// which lacks it is a true answer -- built here as the empty spine of the prefix which
// HAS been written, so that the caller's own extraction fails at the same segment,
// with the same kind, as it would have on the whole document.
//
// The index proves each step of that spine is an object before standing on it (see
// UnwrittenBelow), so a scalar in the way is answered by the document as it always
// was. The one place it can still differ is the message: a patch which deleted an
// ancestor while describing what it deleted leaves that ancestor indexed with
// children, so the answer resolves through it and names a deeper missing field --
// `no field "zz"` under a path the document says is itself gone. The kind, the error
// and the code are the same; only how far it says it got can be generous. A path whose
// every segment has been written is not answered here at all -- that is the case which
// needs the document.
func (s *Storage) AbsentSpineAt(kp string, scopeID *string) (*ir.Node, bool) {
	if kp == "" {
		return nil, false
	}
	if scopeID != nil {
		// A scope's writes are indexed in the same trie on their own timeline, so the
		// commit comparison which proves a path is an object compares two histories
		// and can get the answer right for neither. A scoped read reads.
		return nil, false
	}
	segs := kpath.SplitAll(kp)
	names := make([]string, 0, len(segs))
	for _, seg := range segs {
		name, isField := kpath.SegmentFieldName(seg)
		if !isField {
			return nil, false // keyed or indexed: the document answers those
		}
		names = append(names, name)
	}

	written, ok := s.index.UnwrittenBelow(kp)
	if !ok || written >= len(names) {
		return nil, false // the index cannot say; the document can
	}
	s.readStats.note(ReadNarrowAbsent, kp, 0)

	// The spine of what has been written, ending in the empty object which does not
	// have the segment that fails.
	node := ir.FromMap(map[string]*ir.Node{})
	for i := written - 1; i >= 0; i-- {
		node = ir.FromKeyVals([]ir.KeyVal{{Key: ir.FromString(names[i]), Val: node}})
	}
	return node, true
}
