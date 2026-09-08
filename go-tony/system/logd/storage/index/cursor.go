package index

import (
	"iter"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// Segments answers, in commit order and each log entry once, the segments in [from, to]
// that can affect the subtree at kp: the writes that LAND at an ancestor of kp, since a
// write at an ancestor writes through it, and every segment at kp itself, which is every
// entry that touched kp or anything below it.
//
// It is an iterator and not a slice because its length is the number of writes in range,
// and a signature that returns that as a slice cannot express the bound a read is held to
// (read_write_interface.md): the slice it replaces returned 166 MB before a single log entry
// was read. What is held here is one small list per node on the path to kp -- the node's
// own segments in range, taken under a brief lock -- and those are bounded by the writes to
// that path in the range, not by history.
//
// Nothing below kp is visited. IndexPatch records a segment at every path on the way to
// what a patch writes, so an entry indexed anywhere under kp is indexed at kp: as Spine
// where the patch merely passed through, and otherwise as a write. Both count at kp. At
// an ancestor only a write counts; a patch that passed through an ancestor on its way to
// kp's sibling is described by its own deeper segments and does not concern a read at kp.
// That is the selectivity a narrow read has (ap8ddvp2h12krd43gdn0), and
// TestEveryEntryIsIndexedAtEveryPrefix is what holds the invariant it rests on.
//
// The segments come back with their full paths. A caller that wants the set as a slice
// says so with slices.Collect, and says why.
func (i *Index) Segments(kp string, from, to *int64, scopeID *string) iter.Seq[LogSegment] {
	return func(yield func(LogSegment) bool) {
		inRange := inCommitRange(from, to)
		upTo := commitsUpTo(to)

		// One list per node on the path, root first.
		var lists [][]LogSegment
		var prefix []string
		node, rest := i, kp
		for {
			atKP := rest == ""
			segs := node.segmentsHere(upTo, func(c LogSegment) bool {
				if !atKP && c.Spine {
					return false
				}
				return inRange(c) && matchesScope(c.ScopeID, scopeID)
			})
			full := joinSegments(prefix)
			for j := range segs {
				segs[j].KindedPath = full
			}
			lists = append(lists, segs)
			if atKP {
				break
			}
			first, tail := kpath.Split(rest)
			child := node.childOf(first)
			if child == nil {
				break // kp was never written; its ancestors' writes are all there is
			}
			prefix = append(prefix, first)
			node, rest = child, tail
		}

		// A k-way merge over a handful of sorted lists, deduplicated: one entry is indexed
		// at every level it passes through, and its copies share every field the order
		// compares on, so they arrive together.
		heads := make([]int, len(lists))
		var last segKey
		have := false
		for {
			best := -1
			for k, l := range lists {
				if heads[k] >= len(l) {
					continue
				}
				if best < 0 || LogSegCompare(l[heads[k]], lists[best][heads[best]]) < 0 {
					best = k
				}
			}
			if best < 0 {
				return
			}
			seg := lists[best][heads[best]]
			heads[best]++
			key := segKey{seg.LogFile, seg.LogFileGeneration, seg.LogPosition, seg.StartTx, scopeKey(seg.ScopeID)}
			if have && key == last {
				continue
			}
			last, have = key, true
			if !yield(seg) {
				return
			}
		}
	}
}

// joinSegments renders a path from its segments, the way IndexIterator.Path does.
func joinSegments(segs []string) string {
	if len(segs) == 0 {
		return ""
	}
	result := segs[len(segs)-1]
	for i := len(segs) - 2; i >= 0; i-- {
		result = kpath.Join(segs[i], result)
	}
	return result
}
