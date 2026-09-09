package storage

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/patches"
)

// Scope compaction (rebuild_plan.md phase 7, second half; 5hmq80f3h12krh1mbsn0).
//
// A scope's entries are its layer: a scoped read folds baseline to the commit asked for
// and then the scope's entries, all of them, in order, on top. Nothing materialized stands
// in for them -- a scope's view depends on a baseline that keeps moving -- so compaction
// kept every one until DeleteScope, and a scope that rewrote one field a thousand times
// carried a thousand entries into every read of it.
//
// What a stored scope write IS makes most of them removable. It is absolute
// (api.ValidateForStorage; lower.go): a statement of what a value is at a path, not of how
// to change it. So an entry is DOMINATED when a later entry of the same scope states every
// path it stated, or an ancestor of it, in a way that does not depend on what was there --
// and a dominated entry contributes nothing to the fold from that later entry on. Beyond
// the cutoff, where history is already approximate, it goes.
//
// COVERS. What a later statement at q says about everything at and beneath q:
//
//	total   !insert or !delete: the result at q is the operand, or absence, whatever
//	        was there -- comments, tags and all. A claim is !insert.raw (lower.go).
//	whole   a plain scalar, or an array of plain scalars: the value at q is replaced, and
//	        everything beneath q with it. At q ITSELF the merge may keep a comment or a
//	        tag the earlier statement carried, so a whole cover at the same path dominates
//	        only a statement that carried neither.
//	none    anything else. An array with object elements merges element by element; an
//	        empty object merges nothing away; an operation other than the two above
//	        (!addtag, !comment ...) says something relative to its neighbours. A bare
//	        !raw merges its subtree as data (mergeop/raw.go) and covers what a value of
//	        that shape covers.
//
// A statement is dominated by a total cover at its path or above, by a whole cover
// strictly above it, or -- when it is an untagged, uncommented scalar or array of them, an
// empty object, or a !raw or !delete -- by a whole cover at its own path.
//
// The pass is one walk of the scope's entries, newest first, holding the covers seen so
// far: an entry is dominated if every one of its roots (patches.Roots, the reading the
// processor applies an entry by) is covered, and then its own covers join the set,
// dominated or not. It reads each of the scope's entries once per compaction; after the
// first, what is left to read is the layer and a tail.

type coverStrength int

const (
	coverNone coverStrength = iota
	coverWhole
	coverTotal
)

// covers is what later statements have said, by path.
type covers map[string]coverStrength

func (c covers) add(path string, st coverStrength) {
	if st > c[path] {
		c[path] = st
	}
}

// covered says whether a statement at path, with the given need, is dominated by the
// covers: a total cover at or above it, a whole cover strictly above it, or a whole cover
// at its own path when that is enough for it.
func (c covers) covered(path string, need coverStrength) bool {
	segs := kpath.SplitAll(path)
	for i := 0; i <= len(segs); i++ {
		switch c[joinSegments(segs[:i])] {
		case coverTotal:
			return true
		case coverWhole:
			if i < len(segs) || need == coverWhole {
				return true
			}
		}
	}
	return false
}

// statement classifies a root: what it offers as a cover to statements before it, and
// what it needs from statements after it to be dominated.
func statement(root *ir.Node) (offers, needs coverStrength) {
	commented := root != nil && root.Type == ir.CommentType
	n := ir.Uncomment(root)
	if n == nil {
		return coverNone, coverTotal
	}
	// The operation a node is applied by is the first one in its chain: !insert.raw is an
	// insert, and a label ahead of the operation is the value's.
	switch firstOperator(n.Tag) {
	case "insert":
		// The result is the operand whatever was there (insertOp.Patch applies it
		// against absence); a comment on the wrapper may land with it.
		if commented {
			return coverTotal, coverTotal
		}
		return coverTotal, coverWhole
	case "delete":
		return coverTotal, coverWhole
	case "raw":
		// The escape merges, so the statement is the value it wraps, every tag beneath
		// it a data tag. A data tag AHEAD of the escape rides on the value too.
		pre, _, _, child, err := mergeop.SplitChild(n)
		if err != nil || child == nil {
			return coverNone, coverTotal
		}
		offers, needs = statementData(child, commented)
		if pre != "" && needs < coverTotal {
			needs = coverTotal
		}
		return offers, needs
	case "":
	default:
		return coverNone, coverTotal
	}
	return statementData(n, commented)
}

// statementData classifies a node whose tags are data: what a value of its shape offers
// to the statements before it and needs from the ones after, with nothing to dispatch.
func statementData(n *ir.Node, commented bool) (offers, needs coverStrength) {
	switch n.Type {
	case ir.ObjectType:
		if commented {
			return coverNone, coverTotal
		}
		return coverNone, coverWhole
	case ir.ArrayType:
		if !plainValue(n) {
			return coverNone, coverTotal
		}
	default:
		if n.Tag != "" || commented {
			return coverWhole, coverTotal
		}
	}
	if commented {
		return coverWhole, coverTotal
	}
	return coverWhole, coverWhole
}

// firstOperator is the merge operation a tag chain names first, without its '!', or ""
// when it names none.
func firstOperator(tag string) string {
	for t := tag; t != ""; {
		head, _, rest := ir.TagArgs(t)
		if head == "" {
			return ""
		}
		if mergeop.Lookup(head[1:]) != nil {
			return head[1:]
		}
		if rest == t {
			return ""
		}
		t = rest
	}
	return ""
}

// plainValue is a value with nothing on it that a merge might keep: no tag, no comment,
// and, in an array, no element that is not itself one -- an object element merges rather
// than replaces.
func plainValue(n *ir.Node) bool {
	if n == nil || n.Tag != "" {
		return false
	}
	switch n.Type {
	case ir.CommentType, ir.ObjectType:
		return false
	case ir.ArrayType:
		for _, v := range n.Values {
			if !plainValue(v) {
				return false
			}
		}
	}
	return true
}

// dominatedScopeEntries answers, by log position, which of the candidate entries -- scope
// entries of the inactive log that are beyond the cutoff, each with its scope -- a later
// entry of the same scope dominates. The pass runs over the whole of each scope, since the
// dominating entry may be anywhere later, the active log included.
func (s *Storage) dominatedScopeEntries(inactive dlog.LogFileID, candidates map[int64]*string) (map[int64]bool, error) {
	scopes := map[string]bool{}
	for _, sc := range candidates {
		scopes[*sc] = true
	}
	dominated := map[int64]bool{}
	for scope := range scopes {
		var segs []index.LogSegment
		for seg := range s.index.Segments("", nil, nil, &scope) {
			if seg.ScopeID == nil || *seg.ScopeID != scope || seg.StartCommit == seg.EndCommit || isOverlaySegment(seg) {
				continue
			}
			segs = append(segs, seg)
		}
		cov := covers{}
		for i := len(segs) - 1; i >= 0; i-- {
			seg := segs[i]
			entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
			if err != nil {
				return nil, fmt.Errorf("scope %s: read entry at %s@%d: %w", scope, seg.LogFile, seg.LogPosition, err)
			}
			if entry.Patch == nil {
				continue
			}
			type offer struct {
				path string
				st   coverStrength
			}
			var offers []offer
			roots, all := 0, true
			patches.Roots(entry.Patch, func(node *ir.Node, path string) {
				roots++
				o, need := statement(node)
				if !cov.covered(path, need) {
					all = false
				}
				offers = append(offers, offer{path, o})
			})
			if roots > 0 && all && dlog.LogFileID(seg.LogFile) == inactive && candidates[seg.LogPosition] != nil {
				dominated[seg.LogPosition] = true
			}
			for _, o := range offers {
				if o.st != coverNone {
					cov.add(o.path, o.st)
				}
			}
		}
	}
	return dominated, nil
}
