package index

import (
	"slices"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
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
func IndexPatch(idx *Index, e *dlog.Entry, logFile string, pos int64, txSeq int64, generation int64, diff *ir.Node, schema *api.Schema, scopeID *string) {
	indexPatchRec(idx, e, logFile, pos, txSeq, generation, diff, "", schema, scopeID)
}

func indexPatchRec(idx *Index, e *dlog.Entry, logFile string, pos int64, txSeq int64, generation int64, n *ir.Node, kPath string, schema *api.Schema, scopeID *string) {
	seg := NewLogSegmentFromPatchEntry(e, kPath, logFile, pos, txSeq, generation, scopeID)
	seg.Spine = passesThrough(n)
	idx.Add(seg)

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
			indexPatchRec(idx, e, logFile, pos, txSeq, generation, o.Node,
				kPath+o.Suffix, schema, scopeID)
		}
		return
	}

	// Where the parts of this patch land, which is PatchChildren's single answer --
	// a field is a .field step, an integer-keyed object a {sparse} one, an array [i]
	// unless it is keyed, and a keyed one (key) as ir.ElemKey reads it.
	for _, c := range PatchChildren(n, kPath, schema) {
		indexPatchRec(idx, e, logFile, pos, txSeq, generation, c.Node, c.Path,
			schema, scopeID)
	}
}
