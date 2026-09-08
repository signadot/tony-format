package index

import (
	"iter"
	"math"
	"sort"

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

		// One list per node on the path, root first.
		var lists [][]LogSegment
		var prefix []string
		node, rest := i, kp
		for {
			atKP := rest == ""
			segs := node.segmentsWithin(from, to, func(c LogSegment) bool {
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

// SnapshotAtOrAbove answers the baseline snapshot a read at kp as of `at` seeks to: of
// the snapshots at kp and at each of its ancestors with a commit at or below at, the one
// with the greatest commit, and of two at one commit the deepest. A snapshot is indexed
// only at the path it is of, so the walk is down kp's prefixes, one node at a time under
// that node's own lock; nothing below kp is visited and nothing is collected.
//
// The greatest commit is the right one because what a read folds after the seek is the
// writes since it: a deeper snapshot taken earlier leaves more to fold, and either is
// opened through its own path index at the read's path, so neither is a larger base.
func (i *Index) SnapshotAtOrAbove(kp string, at int64) (LogSegment, bool) {
	var best LogSegment
	found := false
	var prefix []string
	node, rest := i, kp
	for {
		if seg, ok := node.latestSnapshot(at); ok && (!found || seg.StartCommit >= best.StartCommit) {
			seg.KindedPath = joinSegments(prefix)
			best, found = seg, true
		}
		if rest == "" {
			break
		}
		first, tail := kpath.Split(rest)
		child := node.childOf(first)
		if child == nil {
			break
		}
		prefix = append(prefix, first)
		node, rest = child, tail
	}
	return best, found
}

// latestSnapshot is this node's most recent baseline snapshot at or below at. The
// regions' headers say which commit it is at without paging anything: only the region
// that holds it is made resident, and only when there is one to hold.
func (i *Index) latestSnapshot(at int64) (LogSegment, bool) {
	i.RLock()
	var want *region
	var commit int64
	for k := len(i.regions) - 1; k >= 0 && want == nil; k-- {
		reg := i.regions[k]
		if reg.minStart > at {
			continue
		}
		snaps := reg.snaps
		j := sort.Search(len(snaps), func(j int) bool { return snaps[j] > at })
		if j > 0 {
			want, commit = reg, snaps[j-1]
		}
	}
	i.RUnlock()
	if want == nil {
		return LogSegment{}, false
	}
	var found LogSegment
	ok := false
	i.withResident(func(r *region) bool { return r == want }, false, func() {
		target := LogSegment{StartCommit: commit, StartTx: math.MaxInt64, EndCommit: commit, EndTx: math.MaxInt64, KindedPath: "\xff\xff\xff\xff"}
		for it := i.Commits.IterSeek(target, false); it.Valid(); it.Next() {
			seg := it.Value()
			if seg.StartCommit != commit {
				return
			}
			if seg.StartCommit == seg.EndCommit && seg.ScopeID == nil {
				found, ok = seg, true
				return
			}
		}
	})
	return found, ok
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
