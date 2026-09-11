package index

import (
	"slices"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// LogSegment is one index record: the log entry at LogFile:LogPosition, indexed at
// KindedPath, over the commit range [StartCommit, EndCommit].
//
//   - StartCommit == EndCommit: a snapshot, the full state of the subtree at KindedPath
//     as of that commit, indexed at that path only
//   - StartCommit != EndCommit: a patch entry, from the entry's LastCommit to its Commit,
//     indexed at every path on the way to what it writes
//
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
	ScopeOverlay      bool    // the entry is a scope overlay (a cached layer logd no longer writes), not one of its writes
	// Spine says this path is one the patch passed THROUGH on its way to what it
	// actually wrote: a plain container, no operator, with the written values indexed
	// beneath it. A read below such a path is not affected by it -- what it did is
	// described by the segments deeper down -- which is what lets a read at one path
	// skip the writes to its siblings.
	//
	// False is the conservative answer and the one an older persisted index decodes to,
	// so a read including it behaves as reads always did.
	Spine bool
	// Statement says this segment is one of the entry's STATEMENTS: a node the entry
	// states something at, in the reading the fold applies an entry by -- an operation, a
	// leaf, an empty container, a commented node -- and not a path the entry passed through
	// (Spine) or a path inside an operation's operand, which the operation states for it.
	// Offers and Needs are its cover (Cover). A scope's statements are what the footprint
	// holds live (footprint.go).
	Statement bool
	Offers    Cover
	Needs     Cover
}

func (s *LogSegment) String() string {
	as, _ := gomap.ToString(s, gomap.EncodeWire(true))
	return as
}

// SortLogSegments sorts a slice of LogSegment pointers in [LogSegCompare] order.
func SortLogSegments(segments []*LogSegment) {
	// Use the existing LogSegCompare function
	slices.SortFunc(segments, func(a, b *LogSegment) int {
		return LogSegCompare(*a, *b)
	})
}

// WithinCommitRange reports whether a's commit range lies within b's.
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
// it never names -- and neither is a leaf, which is a write. Neither is a node wearing a
// HEAD COMMENT: the fold keeps the earlier node's comment under a later plain write, so
// the comment is something the entry states at that path, and the reading the fold
// applies an entry by (patches.Roots) says the same. Neither is an ARRAY: an array that
// declares no identity is a value and the whole array is the unit (element_identity.md),
// so the entry states the array at its path and its elements are what it states, not
// paths it passed through on the way to them. A keyed array is an object of names in the
// store and never arrives here as an array.
//
// PRESENTATION does not count as a tag here. It is how the container was written, not
// something the patch says about what is under it, and a read below the path cannot see
// it either way. It counted before, and flow style always carries one -- `{b: {c: 1}}`
// parses with !bracket where the same document in block style parses bare -- so a patch
// written in flow, which is every patch a JSON client sends, was never marked as passing
// through anything. The document decided the read cost, and its spelling decided the
// document.
func passesThrough(n *ir.Node) bool {
	if n == nil || n.Type == ir.CommentType {
		return false
	}
	if ir.StripPresentation(n.Tag) != "" {
		return false
	}
	return n.Type == ir.ObjectType && len(n.Fields) > 0
}

// NewLogSegmentFromPatchEntry is the segment of patch entry e at kpath: StartCommit is e's
// LastCommit, which must be set, EndCommit its Commit, and txID both StartTx and EndTx.
// Spine, Statement and the cover are left unset; IndexPatch sets them.
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
	w := &segmentWalk{e: e, logFile: logFile, pos: pos, txSeq: txSeq, generation: generation, scopeID: scopeID, fn: idx.Add}
	w.at(diff, "", asPatch)
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
		w := &segmentWalk{e: e, logFile: logFile, pos: pos, txSeq: TxSeqOf(e), generation: generation, scopeID: e.ScopeID, fn: fn}
		w.at(e.Patch, "", asPatch)
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

// segmentWalk derives an entry's segments: one at every path the entry's structure or
// its operands reach, each saying whether the entry passes through the path (Spine) or
// states something at it (Statement), and what the statement covers.
type segmentWalk struct {
	e          *dlog.Entry
	logFile    string
	pos        int64
	txSeq      int64
	generation int64
	scopeID    *string
	fn         func(*LogSegment)
}

// walkMode says what the nodes beneath mean.
type walkMode int

const (
	// asPatch: ordinary structure; a tag names an operation.
	asPatch walkMode = iota
	// asData: inside a merging !raw. Every tag is data, and the nodes are still the
	// entry's statements -- a raw object merges field by field, so its fields are what
	// it states.
	asData
	// asOperand: inside an operation's operand. The paths are the document's and are
	// recorded, but the OPERATION states them; nothing here is a statement of its own.
	asOperand
)

// at records the segment for n at kPath and everything beneath it.
func (w *segmentWalk) at(n *ir.Node, kPath string, mode walkMode) {
	seg := NewLogSegmentFromPatchEntry(w.e, kPath, w.logFile, w.pos, w.txSeq, w.generation, w.scopeID)
	seg.Spine = passesThrough(n)
	if !seg.Spine && mode != asOperand {
		seg.Statement = true
		seg.Offers, seg.Needs = Classify(n, mode == asData)
	}
	w.fn(seg)
	w.below(n, kPath, mode)
}

// below records the segments BENEATH n: at the paths its own structure reaches, or the
// paths its operand's document values sit at. An operand that sits where its operation
// sits -- OperandPaths' Suffix "" -- is not a second statement about that path, so it is
// walked for what is beneath it and not recorded again.
//
// It was recorded again, and the copies were not harmless. Two segments of one entry at
// one path are EQUAL to the index -- LogSegCompare reads neither Spine nor the position --
// so the tree kept the first, which was the operation's, marked a write. Compaction
// re-indexes a survivor one segment at a time, removing and adding each, and the last
// copy through was the operand's, marked spine where the operand is a plain container. A
// read below a scope's claim then skipped the operator above it at the ancestor, and the
// claim stopped shadowing baseline the moment a compaction moved it
// (TestAClaimShadowsAfterCompaction).
func (w *segmentWalk) below(n *ir.Node, kPath string, mode walkMode) {
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
	// when it has no answer the walk below runs as it always did. Inside a merging
	// !raw nothing is an operation, so nothing there has an operand.
	if mode != asData {
		if ops, known := mergeop.OperandPaths(n); known {
			// Inside an operand everything stays the operation's, a merging !raw's
			// contents included: !insert.raw states its value whole, and the value's
			// fields are not statements of their own.
			childMode := asOperand
			if mode == asPatch && firstOperator(n.Tag) == "raw" {
				childMode = asData
			}
			for _, o := range ops {
				if o.Suffix == "" {
					w.below(o.Node, kPath, childMode)
					continue
				}
				w.at(o.Node, kPath+o.Suffix, childMode)
			}
			return
		}
	}

	// Where the parts of this patch land, which is PatchChildren's single answer --
	// a field is a .field step, an integer-keyed object a {sparse} one, an array [i],
	// and an element of a keyed array the field that is its name. An array's elements
	// are what the ARRAY states (passesThrough): recorded, so a read at an element and
	// the index's proof of absence know them, but not statements of their own.
	if n.Type == ir.ArrayType && mode != asOperand {
		mode = asOperand
	}
	for _, c := range PatchChildren(n, kPath) {
		w.at(c.Node, c.Path, mode)
	}
}
