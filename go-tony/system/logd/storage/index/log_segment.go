package index

import (
	"fmt"
	"slices"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

//tony:schemagen=log-segment
type LogSegment struct {
	StartCommit   int64
	StartTx       int64
	EndCommit     int64
	EndTx         int64
	KindedPath    string   // Full kinded path from root (e.g., "a.b.c", "resources("joe")", "" for root)
	ArrayKey      *ir.Node // Key value for !key arrays (e.g., ir.FromString("joe")) - nil if not keyed
	ArrayKeyField string   // Kpath to key field for !key arrays (e.g., "name", "address.city") - empty if not keyed
	LogFile       string   // "A" or "B" - which log file contains this segment
	LogPosition   int64    // Byte offset in log file
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

func NewLogSegmentFromPatchEntry(e *dlog.Entry, kpath string, logFile string, pos int64, txID int64) *LogSegment {
	// For patches: StartCommit = LastCommit, EndCommit = Commit
	// This represents the range [LastCommit, Commit] that the patch covers
	start := *e.LastCommit
	end := e.Commit
	return &LogSegment{
		StartCommit: start,
		StartTx:     txID,
		EndCommit:   end,
		EndTx:       txID,
		KindedPath:  kpath,
		LogFile:     logFile,
		LogPosition: pos,
	}
}

func IndexPatch(idx *Index, e *dlog.Entry, logFile string, pos int64, txSeq int64, diff *ir.Node) error {
	return indexPatchRec(idx, e, logFile, pos, txSeq, diff, "")
}

func indexPatchRec(idx *Index, e *dlog.Entry, logFile string, pos int64, txSeq int64, n *ir.Node, kPath string) error {
	seg := NewLogSegmentFromPatchEntry(e, kPath, logFile, pos, txSeq)
	idx.Add(seg)

	if n == nil {
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
				if err := indexPatchRec(idx, e, logFile, pos, txSeq, v, nextPath); err != nil {
					return err
				}
			}
			return nil
		}
		for i := range n.Fields {
			field := n.Fields[i]
			val := n.Values[i]
			key := field.String
			nextPath := ""
			if kPath == "" {
				nextPath = key
			} else {
				nextPath = kPath + "." + key
			}
			if err := indexPatchRec(idx, e, logFile, pos, txSeq, val, nextPath); err != nil {
				return err
			}
		}
		return nil
	case ir.ArrayType:
		key, args := ir.TagGet(n.Tag, "key")
		if key == "" {
			for i, v := range n.Values {
				next := fmt.Sprintf("%s[%d]", kPath, i)
				if err := indexPatchRec(idx, e, logFile, pos, txSeq, v, next); err != nil {
					return err
				}
			}
			return nil
		}
		// keys in patches just add the index item associated with the key (key).
		// however, this lacks enforcement of using !key in the stored document
		// TODO enforce !key in stored document with schema and ensure that storage
		// is made aware of this somehow, and reference that here instead of
		// just !key in the patch
		if len(args) != 1 {
			return fmt.Errorf("!key has incorrect form of args %v", args)
		}
		key = args[0]
		for _, v := range n.Values {
			// default to "" for things aren't indexable this way.
			indexVal := ""
			if v.Type == ir.ObjectType {
				keyVal := ir.Get(n, key)
				if keyVal != nil {
					indexVal = keyVal.String
				}
			}
			next := fmt.Sprintf("%s%s", kPath, kpath.Key(indexVal).SegmentString())
			if err := indexPatchRec(idx, e, logFile, pos, txSeq, v, next); err != nil {
				return err
			}
		}
	}
	return nil
}
