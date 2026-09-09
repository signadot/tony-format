package index

import (
	"slices"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

//tony:schemagen=log-segment
type LogSegment struct {
	StartCommit       int64
	StartTx           int64
	EndCommit         int64
	EndTx             int64
	KindedPath        string  // Full kinded path from root: "a.b.c", `items."(sku=A)"`, "" for root
	LogFile           string  // "A" or "B" - which log file contains this segment
	LogPosition       int64   // Byte offset in log file
	LogFileGeneration int64   // Generation of log file when indexed - used to detect compaction
	ScopeID           *string // nil = baseline, non-nil = scope-specific data
	ScopeOverlay      bool    // the entry is a scope's materialized ownership, not one of its writes
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

// NewSnapshotSegment is the segment of a snapshot OF kp at commit: StartCommit ==
// EndCommit, at the path whose subtree the snapshot's event stream is. That path is the
// only place it is indexed, and where a read at or below it seeks to it
// (SnapshotAtOrAbove); a read above it does not see it and folds the writes instead.
func NewSnapshotSegment(commit int64, kp, logFile string, pos, generation int64, scopeID *string) *LogSegment {
	return &LogSegment{
		StartCommit:       commit,
		EndCommit:         commit,
		StartTx:           0,
		EndTx:             0,
		KindedPath:        kp,
		LogFile:           logFile,
		LogPosition:       pos,
		LogFileGeneration: generation,
		ScopeID:           scopeID,
	}
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
//
// PRESENTATION does not count as a tag here. It is how the container was written, not
// something the patch says about what is under it, and a read below the path cannot see
// it either way. It counted before, and flow style always carries one -- `{b: {c: 1}}`
// parses with !bracket where the same document in block style parses bare -- so a patch
// written in flow, which is every patch a JSON client sends, was never marked as passing
// through anything. The document decided the read cost, and its spelling decided the
// document.
func passesThrough(n *ir.Node) bool {
	if n == nil {
		return false
	}
	n = ir.Uncomment(n)
	if ir.StripPresentation(n.Tag) != "" {
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

// IndexPatch records where a stored delta lands, and CANNOT FAIL -- deliberately, and
// the signature says so.
//
// Indexing happens after the log append, because a segment records the position the
// append returns, so anything fallible here would be fallible with the record already
// on disk: the caller is told the commit failed, replay reads the entry back, and the
// two disagree about whether it happened. During a schema migration it would be worse
// than that. Every commit is then indexed twice, once under each schema, and
// CompleteMigration installs the pending index as the live one verbatim -- so an entry
// this skipped would be missing from the index the store then runs on, permanently
// (tkn7ptxch12krgzma9mg).
//
// The walk has no failure to report: it derives paths and adds segments, and a shape it
// does not understand is a shape it descends no further into. Keep it that way. If
// something here ever needs to refuse, the refusal belongs where the delta is BUILT,
// before the append, not here.
func IndexPatch(idx *Index, e *dlog.Entry, logFile string, pos int64, txSeq int64, generation int64, diff *ir.Node, scopeID *string) {
	eachPatchSegment(e, logFile, pos, txSeq, generation, diff, "", scopeID, idx.Add)
}

// EachSegment hands fn every segment an entry is indexed at, derived from the entry
// alone: a patch's, at every path on the way to what it writes -- IndexPatch's own walk
// -- or a snapshot's, at the path it is of. It is how an entry leaves the index or moves
// within it (storage.Compact) without the index being asked for every segment it holds:
// the walk that put the segments in derives them again, and a segment is removed by its
// key, which the walk reproduces.
func EachSegment(e *dlog.Entry, logFile string, pos, generation int64, fn func(*LogSegment)) {
	switch {
	case e.Patch != nil:
		eachPatchSegment(e, logFile, pos, TxSeqOf(e), generation, e.Patch, "", e.ScopeID, fn)
	case e.SnapPos != nil:
		fn(NewSnapshotSegment(e.Commit, SnapPathOf(e), logFile, pos, generation, e.ScopeID))
	}
}

// TxSeqOf is the transaction sequence an entry is indexed under: its transaction's id,
// and 0 for an entry with none, which is what a snapshot has.
func TxSeqOf(e *dlog.Entry) int64 {
	if e.TxSource != nil {
		return e.TxSource.TxID
	}
	return 0
}

// SnapPathOf is the path a snapshot entry is of; "" is the root.
func SnapPathOf(e *dlog.Entry) string {
	if e.SnapPath != nil {
		return *e.SnapPath
	}
	return ""
}

func eachPatchSegment(e *dlog.Entry, logFile string, pos int64, txSeq int64, generation int64, n *ir.Node, kPath string, scopeID *string, fn func(*LogSegment)) {
	seg := NewLogSegmentFromPatchEntry(e, kPath, logFile, pos, txSeq, generation, scopeID)
	seg.Spine = passesThrough(n)
	fn(seg)
	eachPatchBelow(e, logFile, pos, txSeq, generation, n, kPath, scopeID, fn)
}

// eachPatchBelow records the segments BENEATH n: at the paths its own structure reaches,
// or the paths its operand's document values sit at. An operand that sits where its
// operation sits -- OperandPaths' Suffix "" -- is not a second statement about that path,
// so it is walked for what is beneath it and not recorded again.
//
// It was recorded again, and the copies were not harmless. Two segments of one entry at
// one path are EQUAL to the index -- LogSegCompare reads neither Spine nor the position --
// so the tree kept the first, which was the operation's, marked a write. Compaction
// re-indexes a survivor one segment at a time, removing and adding each, and the last
// copy through was the operand's, marked spine where the operand is a plain container. A
// read below a scope's claim then skipped the operator above it at the ancestor, and the
// claim stopped shadowing baseline the moment a compaction moved it
// (TestAClaimShadowsAfterCompaction).
func eachPatchBelow(e *dlog.Entry, logFile string, pos int64, txSeq int64, generation int64, n *ir.Node, kPath string, scopeID *string, fn func(*LogSegment)) {
	if n == nil {
		return
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
			if o.Suffix == "" {
				eachPatchBelow(e, logFile, pos, txSeq, generation, o.Node, kPath, scopeID, fn)
				continue
			}
			eachPatchSegment(e, logFile, pos, txSeq, generation, o.Node,
				kPath+o.Suffix, scopeID, fn)
		}
		return
	}

	// Where the parts of this patch land, which is PatchChildren's single answer --
	// a field is a .field step, an integer-keyed object a {sparse} one, an array [i],
	// and an element of a keyed array the field that is its name.
	for _, c := range PatchChildren(n, kPath) {
		eachPatchSegment(e, logFile, pos, txSeq, generation, c.Node, c.Path, scopeID, fn)
	}
}
