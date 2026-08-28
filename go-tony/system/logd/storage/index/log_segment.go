package index

import (
	"fmt"
	"slices"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

//tony:schemagen=log-segment
type LogSegment struct {
	StartCommit       int64
	StartTx           int64
	EndCommit         int64
	EndTx             int64
	KindedPath        string   // Full kinded path from root (e.g., "a.b.c", "resources("joe")", "" for root)
	ArrayKey          *ir.Node // Key value for !key arrays (e.g., ir.FromString("joe")) - nil if not keyed
	ArrayKeyField     string   // Kpath to key field for !key arrays (e.g., "name", "address.city") - empty if not keyed
	LogFile           string   // "A" or "B" - which log file contains this segment
	LogPosition       int64    // Byte offset in log file
	LogFileGeneration int64    // Generation of log file when indexed - used to detect compaction
	ScopeID           *string  // nil = baseline, non-nil = scope-specific data
	ScopeOverlay      bool     // the entry is a scope's materialized ownership, not one of its writes
	// Spine says this path is one the patch passed THROUGH on its way to what it
	// actually wrote: a plain container, no operator, with the written values indexed
	// beneath it. A read below such a path is not affected by it -- what it did is
	// described by the segments deeper down -- which is what lets a read at one path
	// skip the writes to its siblings.
	//
	// False is the conservative answer and the one an older persisted index decodes to,
	// so a read including it behaves as reads always did.
	Spine bool
	// Semantics:
	// - StartCommit == EndCommit: snapshot (full state at that commit)
	// - StartCommit != EndCommit: diff (incremental changes over commit range)
}

func (s *LogSegment) String() string {
	as, _ := gomap.ToString(s, gomap.EncodeWire(true))
	return as
}

// SortLogSegments sorts a slice of LogSegment pointers by commit count, then tx.
func SortLogSegments(segments []*LogSegment) {
	// Use the existing LogSegCompare function
	slices.SortFunc(segments, func(a, b *LogSegment) int {
		return LogSegCompare(*a, *b)
	})
}

func WithinCommitRange(a, b *LogSegment) bool {
	if a.StartCommit < b.StartCommit {
		return false
	}
	if a.EndCommit > b.EndCommit {
		return false
	}
	return true
}

// PointLogSegment creates a LogSegment for a patch at the given commit.
// Assumes LastCommit = commit-1, so StartCommit = LastCommit = commit-1, EndCommit = commit.
// For test purposes, this represents a patch where Commit - LastCommit == 1.
func PointLogSegment(commit, txSeq int64, kpath string) *LogSegment {
	lastCommit := commit - 1
	if commit == 1 {
		lastCommit = 0
	}
	// StartCommit = LastCommit, EndCommit = Commit for patches
	return &LogSegment{
		StartCommit: lastCommit,
		StartTx:     txSeq,
		EndCommit:   commit,
		EndTx:       txSeq,
		KindedPath:  kpath,
		LogFile:     "A",
		LogPosition: 0,
	}
}

// passesThrough says whether a patch node is one the patch merely descends through:
// a container with contents and no operator on it, whose effect is entirely described
// by what is indexed beneath it. An operator is not descended through -- a !replace or
// a !delete at a path states the whole value there, including for paths under it that
// it never names -- and neither is a leaf, which is a write.
func passesThrough(n *ir.Node) bool {
	if n == nil {
		return false
	}
	n = ir.Uncomment(n)
	if n.Tag != "" {
		return false
	}
	switch n.Type {
	case ir.ObjectType:
		return len(n.Fields) > 0
	case ir.ArrayType:
		return len(n.Values) > 0
	}
	return false
}

func NewLogSegmentFromPatchEntry(e *dlog.Entry, kpath string, logFile string, pos int64, txID int64, generation int64, scopeID *string) *LogSegment {
	// For patches: StartCommit = LastCommit, EndCommit = Commit
	// This represents the range [LastCommit, Commit] that the patch covers
	start := *e.LastCommit
	end := e.Commit
	return &LogSegment{
		StartCommit:       start,
		StartTx:           txID,
		EndCommit:         end,
		EndTx:             txID,
		KindedPath:        kpath,
		LogFile:           logFile,
		LogPosition:       pos,
		LogFileGeneration: generation,
		ScopeID:           scopeID,
		ScopeOverlay:      e.ScopeOverlay,
	}
}

func IndexPatch(idx *Index, e *dlog.Entry, logFile string, pos int64, txSeq int64, generation int64, diff *ir.Node, schema *api.Schema, scopeID *string) error {
	return indexPatchRec(idx, e, logFile, pos, txSeq, generation, diff, "", schema, scopeID)
}

func indexPatchRec(idx *Index, e *dlog.Entry, logFile string, pos int64, txSeq int64, generation int64, n *ir.Node, kPath string, schema *api.Schema, scopeID *string) error {
	seg := NewLogSegmentFromPatchEntry(e, kPath, logFile, pos, txSeq, generation, scopeID)
	seg.Spine = passesThrough(n)
	idx.Add(seg)

	if n == nil {
		return nil
	}

	// A head comment wraps the value it precedes, so a patch carrying comments has
	// a CommentType between a field and its contents. The switch below asks what
	// KIND of node this is, and a comment is not a kind of container: without this
	// the recursion stopped at the wrapper and every path beneath it went
	// unrecorded. One comment at the top of a patch indexed the root and nothing
	// else, so a watch on a path inside it did not see the commit -- data lost, not
	// comments. It is the index's question, not the format's, so the wrapper is
	// looked through rather than refused (3cdjz00jh12krns4g1n0).
	n = ir.Uncomment(n)

	// An operand is not ordinary structure. Descending into one recorded paths the
	// document does not have -- a write of {a: !comment {head: ["# note"]}} indexed
	// a.head and a.head[0] -- and, worse, would record a value an operand carries at
	// a path below where it actually sits. mergeop.OperandPaths is the one place that
	// knows which parts of which operand are document values and where each sits;
	// when it has no answer the walk below runs as it always did.
	if ops, known := mergeop.OperandPaths(n); known {
		for _, o := range ops {
			if err := indexPatchRec(idx, e, logFile, pos, txSeq, generation, o.Node,
				kPath+o.Suffix, schema, scopeID); err != nil {
				return err
			}
		}
		return nil
	}

	switch n.Type {
	case ir.ObjectType:
		if len(n.Fields) == 0 {
			return nil
		}
		if n.Fields[0].Type == ir.NumberType {
			for i, f := range n.Fields {
				v := n.Values[i]
				nextPath := fmt.Sprintf("%s{%d}", kPath, *f.Int64)
				if err := indexPatchRec(idx, e, logFile, pos, txSeq, generation, v, nextPath, schema, scopeID); err != nil {
					return err
				}
			}
			return nil
		}
		for i := range n.Fields {
			field := n.Fields[i]
			val := n.Values[i]
			key := field.String
			nextPath := kpath.ChildField(kPath, key)
			if err := indexPatchRec(idx, e, logFile, pos, txSeq, generation, val, nextPath, schema, scopeID); err != nil {
				return err
			}
		}
		return nil
	case ir.ArrayType:
		// Check schema first for key field
		keyField, keyed := "", false
		if schema != nil {
			keyField = schema.LookupKeyField(kPath)
			keyed = keyField != ""
		}
		// Fall back to the !key tag the patch carries.  keyed is tracked apart
		// from keyField because a bare !key keys its elements by themselves,
		// which is an empty field and still a keyed list.
		if !keyed {
			keyField, keyed = n.KeyField()
		}

		// Not a keyed array - use positional indexing
		if !keyed {
			for i, v := range n.Values {
				next := fmt.Sprintf("%s[%d]", kPath, i)
				if err := indexPatchRec(idx, e, logFile, pos, txSeq, generation, v, next, schema, scopeID); err != nil {
					return err
				}
			}
			return nil
		}

		// Keyed array - index by key value.  The key comes from ir.ElemKey, the
		// same reading a walk gives a (key) segment, so a path this records is a
		// path a reader can follow back.
		for _, v := range n.Values {
			// default to "" for things aren't indexable this way.
			indexVal, _ := ir.ElemKey(v, keyField)
			next := fmt.Sprintf("%s%s", kPath, kpath.Key(indexVal).SegmentString())
			if err := indexPatchRec(idx, e, logFile, pos, txSeq, generation, v, next, schema, scopeID); err != nil {
				return err
			}
		}
	}
	return nil
}
