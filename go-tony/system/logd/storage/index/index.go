package index

import (
	"cmp"
	"slices"
	"sync"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

type Index struct {
	sync.RWMutex
	PathKey  string // eg "" for root
	Commits  *Tree[LogSegment]
	Children map[string]*Index // map from subdir names to sub indices
}

func NewIndex(pathKey string) *Index {
	return &Index{
		PathKey: pathKey,
		Commits: NewTree(func(a, b LogSegment) bool {
			if a.StartCommit < b.StartCommit {
				return true
			}
			if a.StartCommit > b.StartCommit {
				return false
			}
			if a.StartTx < b.StartTx {
				return true
			}
			if a.StartTx > b.StartTx {
				return false
			}
			// Compare KindedPath using KPath.Compare()
			aKp, errA := kpath.Parse(a.KindedPath)
			bKp, errB := kpath.Parse(b.KindedPath)
			// Fallback to string comparison if parsing fails
			if errA != nil || errB != nil {
				if a.KindedPath != b.KindedPath {
					return a.KindedPath < b.KindedPath
				}
				return compareScopeID(a.ScopeID, b.ScopeID) < 0
			}
			// Handle nil cases
			if aKp == nil && bKp == nil {
				return compareScopeID(a.ScopeID, b.ScopeID) < 0
			}
			if aKp == nil {
				return true // Empty < non-empty
			}
			if bKp == nil {
				return false // Non-empty > empty
			}
			n := aKp.Compare(bKp)
			if n != 0 {
				return n < 0
			}
			return compareScopeID(a.ScopeID, b.ScopeID) < 0
		}),
		Children: map[string]*Index{},
	}
}

// A lock on this node is held to READ THIS NODE and released before descending. Holding
// it across the recursion makes the walk's cost the writers' cost: a commit's Add needs
// the root's write lock, and a reader holding the root's read lock for the length of a
// whole-index walk stops every write for that long. Staging measured a single commit
// paying 5.8s in its index phase, inside a snapshot window -- compaction was walking the
// index while writes were landing (kds4sx3bh12krdrkghn0, and the same shape as the
// persist stall in v0.0.169).
//
// childrenOf and childOf are how a walk gets what it descends into: named under the
// lock, followed after it.

type namedChild struct {
	name  string
	index *Index
}

func (i *Index) childrenOf() []namedChild {
	i.RLock()
	defer i.RUnlock()
	res := make([]namedChild, 0, len(i.Children))
	for name, c := range i.Children {
		res = append(res, namedChild{name: name, index: c})
	}
	return res
}

func (i *Index) childOf(name string) *Index {
	i.RLock()
	defer i.RUnlock()
	return i.Children[name]
}

// segmentsHere collects this node's OWN segments, under a brief lock.
func (i *Index) segmentsHere(rng func(LogSegment) int, keep func(LogSegment) bool) []LogSegment {
	i.RLock()
	defer i.RUnlock()
	res := []LogSegment{}
	i.Commits.Range(func(c LogSegment) bool {
		if keep == nil || keep(c) {
			res = append(res, c)
		}
		return true
	}, rng)
	return res
}

func (i *Index) Add(seg *LogSegment) {
	i.Lock()
	defer i.Unlock()
	if seg.KindedPath == "" {
		i.Commits.Insert(*seg)
		return
	}
	// Split kpath into first segment and rest for navigation
	firstSegment, restPath := kpath.Split(seg.KindedPath)
	child := i.Children[firstSegment]
	if child == nil {
		child = NewIndex(firstSegment)
		i.Children[firstSegment] = child
	}
	// Create a copy with relative path for recursive call
	// (but we'll store the full path, so create new segment with restPath)
	segCopy := *seg
	segCopy.KindedPath = restPath
	child.Add(&segCopy)
}

func (i *Index) Remove(seg *LogSegment) bool {
	if seg.KindedPath == "" {
		i.Lock()
		defer i.Unlock()
		return i.Commits.Remove(*seg)
	}
	firstSegment, restPath := kpath.Split(seg.KindedPath)
	i.RLock()
	defer i.RUnlock()
	c := i.Children[firstSegment]
	if c == nil {
		return false
	}
	// Create a copy with relative path for recursive call
	segCopy := *seg
	segCopy.KindedPath = restPath
	res := c.Remove(&segCopy)
	// nb low grade mem leak when c empty after remove
	return res
}

// LookupRange finds segments in the given commit range, at or above kp: a write
// above kp writes through it.  A segment appears once per path it was indexed at,
// which is what callers reading the paths depend on.
//
// It does NOT descend below kp.  For "everything which can affect the subtree at
// kp", each entry once, see LookupSubtree.
//
// If scopeID is nil, returns only baseline segments.
// If scopeID is non-nil, returns baseline + matching scope segments.
func (i *Index) LookupRange(kp string, from, to *int64, scopeID *string) []LogSegment {
	inRange := inCommitRange(from, to)
	res := i.segmentsHere(commitsUpTo(to), func(c LogSegment) bool {
		return inRange(c) && matchesScope(c.ScopeID, scopeID)
	})
	if kp == "" {
		slices.SortFunc(res, LogSegCompare)
		return res
	}
	// Split kpath to navigate hierarchy. The descent happens with this node's lock
	// released -- see childOf.
	firstSegment, restPath := kpath.Split(kp)
	c := i.childOf(firstSegment)
	if c == nil {
		return res
	}
	res = appendRelative(res, firstSegment, c.LookupRange(restPath, from, to, scopeID))
	slices.SortFunc(res, LogSegCompare)
	return res
}

// UnwrittenBelow answers where kp stops having been written, when the index can
// prove both halves of that: how many leading segments name paths a patch has
// touched, and that each of those is an OBJECT which the rest of kp is simply not in.
// ok is false when it cannot prove it, and then the caller reads the document as
// before.
//
// The trie holds a node for every path any patch has ever touched (IndexPatch indexes
// a patch at every path inside it), so a segment with no node has never been written
// and no read can find anything there. That much is easy. The hard half is what sits
// at the last written segment: the trie remembers that a path was written, not what
// was written to it, and "no field x" and "that is a String, not an object" are
// different answers to a client.
//
// So each node on the way is required to prove it is an object, and it proves it the
// only way the index can: it has a child written no earlier than the newest write to
// the node itself. A patch which had put a scalar there -- or deleted it -- would be
// a write to the node with nothing indexed beneath it at that commit, and the proof
// fails, and the caller reads.
//
// What it cannot see is deletion: a path written and later deleted keeps its node, so
// a depth here says "never written", never "present" (ap8ddvp2h12krd43gdn0).
func (i *Index) UnwrittenBelow(kp string) (depth int, ok bool) {
	if kp == "" {
		return 0, false // the root is not below anything
	}
	node := i
	for rest := kp; rest != ""; {
		first, tail := kpath.Split(rest)
		child := node.childIndex(first)
		if !node.provenObject() {
			return 0, false // something may sit at node which is not an object
		}
		if child == nil {
			return depth, true // kp stops here, in an object which does not have it
		}
		node, depth, rest = child, depth+1, tail
	}
	return depth, false // every segment written: only the document knows what is there
}

// provenObject says whether the newest write to this path wrote INSIDE it, which is
// something only an object can have happen to it. A patch putting a scalar here -- or
// deleting this -- indexes at this path and nothing under it, so it leaves every child
// older, and the answer is no.
//
// It asks for ANY child, not the one the path descends into. A path is written by
// every patch which passes through it, so requiring the child on the way would fail
// the moment a sibling was written later, which says nothing about the shape here.
func (i *Index) provenObject() bool {
	mine, has := i.newestCommit()
	if !has {
		return false // nothing written here at all; the caller cannot lean on it
	}
	i.RLock()
	children := make([]*Index, 0, len(i.Children))
	for _, c := range i.Children {
		children = append(children, c)
	}
	i.RUnlock()
	for _, c := range children {
		if cc, has := c.newestCommit(); has && cc >= mine {
			return true
		}
	}
	return false
}

// newestCommit is the commit of the last write indexed AT this path.
func (i *Index) newestCommit() (int64, bool) {
	i.RLock()
	defer i.RUnlock()
	for seg := range i.Commits.Commits(Down) {
		return seg.StartCommit, true
	}
	return 0, false
}

func (i *Index) childIndex(name string) *Index {
	i.RLock()
	defer i.RUnlock()
	return i.Children[name]
}

// DropFrom forgets every segment recorded in logFile at or beyond pos, and answers how
// many it forgot.
//
// It is the repair for a log whose framing broke: what lies past an unreadable record
// cannot be found, so an index entry pointing there names data no read can produce.
// Keeping those entries makes every read and every write which needs one fail --
// verifying a write reads the state -- which is a store that opens and can do nothing.
// Dropping them makes the index describe what the store can actually read
// (t96b5ejqh12krprjghn0).
func (i *Index) DropFrom(logFile string, pos int64) int {
	i.Lock()
	var doomed []LogSegment
	i.Commits.All(func(c LogSegment) bool {
		if c.LogFile == logFile && c.LogPosition >= pos {
			doomed = append(doomed, c)
		}
		return true
	})
	dropped := 0
	for _, c := range doomed {
		if i.Commits.Remove(c) {
			dropped++
		}
	}
	children := make([]*Index, 0, len(i.Children))
	for _, c := range i.Children {
		children = append(children, c)
	}
	i.Unlock()

	for _, c := range children {
		dropped += c.DropFrom(logFile, pos)
	}
	return dropped
}

// LookupSubtree answers the distinct log entries in the commit range which can
// affect the subtree at kp: the ones written AT or ABOVE it, since a write above
// writes through it, and the ones written BELOW it, since they are part of the
// subtree.  Each entry is answered ONCE, whichever of those it is, with the highest
// path it was indexed at -- a reader applies an entry once and reads it from the log
// by position, so a segment per path it touches is a repeat.
//
// This is the query a read at a path needs, and its absence is why reads were taken
// at the root instead. A patch is indexed at every path inside it (IndexPatch), so
// LookupRange at a narrow path answered the same set as the root with each entry
// repeated once per level -- no selectivity, several times the cost
// (ap8ddvp2h12krd43gdn0). Selectivity comes with indexing a patch at what it writes
// rather than at every level; the descent here is what makes that possible without
// losing the writes below.
func (i *Index) LookupSubtree(kp string, from, to *int64, scopeID *string) []LogSegment {
	res := i.lookupSubtree(kp, from, to, scopeID, true)
	dedupEntries(&res)
	slices.SortFunc(res, LogSegCompare)
	return res
}

// lookupSubtree collects without sorting or deduping, so the recursion pays for
// neither. scoped says whether scopeID filters at all -- LookupRangeAll asks for
// every scope, which is a different question from asking for the baseline.
func (i *Index) lookupSubtree(kp string, from, to *int64, scopeID *string, scoped bool) []LogSegment {
	res := []LogSegment{}
	i.RLock()
	// Above the queried path, only writes which LAND here count. A patch which merely
	// passed through on its way to a sibling is described by its own deeper segments,
	// and taking it here is what made a narrow read replay every commit in the store:
	// every patch is indexed at the root, so every read at every path collected all of
	// them. Measured on staging, a narrow read averaged three seconds
	// (ap8ddvp2h12krd43gdn0).
	ancestor := kp != ""
	inRange := inCommitRange(from, to)
	i.Commits.Range(func(c LogSegment) bool {
		if ancestor && c.Spine {
			return true
		}
		if !inRange(c) {
			return true
		}
		if !scoped || matchesScope(c.ScopeID, scopeID) {
			res = append(res, c)
		}
		return true
	}, commitsUpTo(to))

	i.RUnlock()

	// Everything below descends with this node's lock RELEASED: a walk holding it is a
	// walk every write waits out (see childrenOf).
	if kp == "" {
		// At the queried path: everything below it is part of the subtree.
		for _, child := range i.childrenOf() {
			res = appendRelative(res, child.name, child.index.lookupSubtree("", from, to, scopeID, scoped))
		}
		return res
	}

	// Still walking down to the queried path.  This node's own segments were
	// collected above, since a write here writes through kp.
	firstSegment, restPath := kpath.Split(kp)
	c := i.childOf(firstSegment)
	if c == nil {
		return res
	}
	return appendRelative(res, firstSegment, c.lookupSubtree(restPath, from, to, scopeID, scoped))
}

// appendRelative adds the child's segments to res, restoring the segment path the
// child answered relative to itself.
func appendRelative(res []LogSegment, name string, cRes []LogSegment) []LogSegment {
	for j := range cRes {
		seg := cRes[j]
		if seg.KindedPath == "" {
			seg.KindedPath = name
		} else {
			seg.KindedPath = kpath.Join(name, seg.KindedPath)
		}
		res = append(res, seg)
	}
	return res
}

// dedupEntries keeps one segment per log entry, whatever paths it was indexed at.
// An entry is identified by where it sits in the log, which is what a reader reads
// from: two segments naming the same position are one write seen from two paths, and
// applying it twice is what made a narrow read cost several times a wide one.
//
// The path kept is the highest the entry was indexed at, which is the one a reader
// can use to decide what the entry writes through.
func dedupEntries(res *[]LogSegment) {
	seen := make(map[segKey]int, len(*res))
	out := (*res)[:0]
	for _, seg := range *res {
		k := segKey{seg.LogFile, seg.LogFileGeneration, seg.LogPosition, seg.StartTx, scopeKey(seg.ScopeID)}
		if at, dup := seen[k]; dup {
			if len(seg.KindedPath) < len(out[at].KindedPath) {
				out[at].KindedPath = seg.KindedPath
			}
			continue
		}
		seen[k] = len(out)
		out = append(out, seg)
	}
	*res = out
}

type segKey struct {
	logFile    string
	generation int64
	position   int64
	startTx    int64
	scope      string
}

func scopeKey(s *string) string {
	if s == nil {
		return ""
	}
	return "s:" + *s
}

// matchesScope returns true if the segment should be included for the given scopeID.
// - If request scopeID is nil (baseline read): only include baseline segments (seg.ScopeID == nil)
// - If request scopeID is non-nil (scope read): include baseline + matching scope segments
func matchesScope(segScopeID, reqScopeID *string) bool {
	if reqScopeID == nil {
		// Baseline read: only baseline segments
		return segScopeID == nil
	}
	// Scope read: baseline + matching scope
	if segScopeID == nil {
		return true // Include baseline
	}
	return *segScopeID == *reqScopeID
}

// LookupRangeAll returns all segments in the given range regardless of scope.
// This is used for internal operations like computing max commit.
func (i *Index) LookupRangeAll(kp string, from, to *int64) []LogSegment {
	i.RLock()
	defer i.RUnlock()
	res := []LogSegment{}
	inRange := inCommitRange(from, to)
	i.Commits.Range(func(c LogSegment) bool {
		if inRange(c) {
			res = append(res, c)
		}
		return true
	}, commitsUpTo(to))
	if kp == "" {
		slices.SortFunc(res, LogSegCompare)
		return res
	}
	firstSegment, restPath := kpath.Split(kp)
	c := i.Children[firstSegment]
	if c == nil {
		return res
	}
	res = appendRelative(res, firstSegment, c.LookupRangeAll(restPath, from, to))
	slices.SortFunc(res, LogSegCompare)
	return res
}

// AllSegments returns every segment in the index, at every path it is indexed at, with
// KindedPath set to the full path rather than the remainder relative to its node.
//
// This is deliberately different from LookupRangeAll(""), which returns only the root
// node's own commits: that one descends solely along a path it is given, so it cannot see
// a segment indexed below the root. A log entry is indexed at the root AND at every path
// inside its patch (indexPatchRec), and all of those copies name the same log position, so
// anything maintaining positions has to reach all of them.
func (i *Index) AllSegments() []LogSegment {
	res := i.segmentsHere(commitsUpTo(nil), nil)
	for _, child := range i.childrenOf() {
		pathKey, c := child.name, child.index
		for _, seg := range c.AllSegments() {
			if seg.KindedPath == "" {
				seg.KindedPath = pathKey
			} else {
				seg.KindedPath = kpath.Join(pathKey, seg.KindedPath)
			}
			res = append(res, seg)
		}
	}
	slices.SortFunc(res, LogSegCompare)
	return res
}

// LookupWithin finds all segments at the given kpath where the commit is within
// the segment's commit range (StartCommit <= commit <= EndCommit).
// Returns ancestors and exact matches, just like LookupRange.
// If scopeID is nil, returns only baseline segments.
// If scopeID is non-nil, returns baseline + matching scope segments.
func (i *Index) LookupWithin(kp string, commit int64, scopeID *string) []LogSegment {
	res := i.segmentsHere(commitsUpTo(&commit), func(c LogSegment) bool {
		return c.StartCommit <= commit && commit <= c.EndCommit && matchesScope(c.ScopeID, scopeID)
	})
	if kp == "" {
		slices.SortFunc(res, LogSegCompare)
		return res
	}
	firstSegment, restPath := kpath.Split(kp)
	c := i.childOf(firstSegment)
	if c == nil {
		return res
	}
	cRes := c.LookupWithin(restPath, commit, scopeID)
	for j := range cRes {
		seg := cRes[j]
		if seg.KindedPath == "" {
			seg.KindedPath = firstSegment
		} else {
			seg.KindedPath = kpath.Join(firstSegment, seg.KindedPath)
		}
		res = append(res, seg)
	}
	slices.SortFunc(res, LogSegCompare)
	return res
}

// withinFunc returns a range function that matches segments containing the given commit.
// LogSegCompare compares 2 log segments by their
// start commit, start-tx, end-commit, end-tx, and path.
func LogSegCompare(a, b LogSegment) int {
	n := cmp.Compare(a.StartCommit, b.StartCommit)
	if n != 0 {
		return n
	}
	n = cmp.Compare(a.StartTx, b.StartTx)
	if n != 0 {
		return n
	}
	n = cmp.Compare(a.EndCommit, b.EndCommit)
	if n != 0 {
		return n
	}
	n = cmp.Compare(a.EndTx, b.EndTx)
	if n != 0 {
		return n
	}
	// Compare KindedPath using KPath.Compare()
	aKp, errA := kpath.Parse(a.KindedPath)
	bKp, errB := kpath.Parse(b.KindedPath)
	// Fallback to string comparison if parsing fails
	if errA != nil || errB != nil {
		n = cmp.Compare(a.KindedPath, b.KindedPath)
		if n != 0 {
			return n
		}
		return compareScopeID(a.ScopeID, b.ScopeID)
	}
	// Handle nil cases (empty paths)
	if aKp == nil && bKp == nil {
		return compareScopeID(a.ScopeID, b.ScopeID)
	}
	if aKp == nil {
		return -1 // Empty path < non-empty
	}
	if bKp == nil {
		return 1 // Non-empty > empty
	}
	n = aKp.Compare(bKp)
	if n != 0 {
		return n
	}
	return compareScopeID(a.ScopeID, b.ScopeID)
}

// compareScopeID compares two scope IDs.
// nil (baseline) < any non-nil scope ID, then string comparison.
func compareScopeID(a, b *string) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1 // baseline < scope
	}
	if b == nil {
		return 1 // scope > baseline
	}
	return cmp.Compare(*a, *b)
}

// commitsUpTo is the walk's BOUND for a commit range, and only the upper half of one,
// because only the upper half can be answered from the order segments are kept in.
//
// A range is asked in terms of EndCommit -- the commit of the patch -- and the tree is
// ordered by StartCommit. A walk may only prune on a key the order is by, or it prunes
// on a value that does not vary with position. Above the range that is sound in one
// direction: EndCommit >= StartCommit always, so a segment starting past `to` ends past
// it, and so does everything ordered after it.
//
// Below the range it is not sound at all, and pruning there is what this used to do. A
// segment starting earlier may still end inside, so the segments in range are not a
// contiguous run and the walk cannot find them by looking for one. With
//
//	[2,3]tx3  [3,4]tx-1  [3,3]tx0  [3,4]tx4  [4,5]tx5
//
// -- which is what an overlay and the writes around it produce -- a lookup for
// EndCommit in [4,5] found [3,4], stopped at [3,3] as though it were past the range,
// and answered [3,4] alone. The scope read built on it lost the write that followed an
// overlay, and the write reappeared once a later commit landed (tmwq9mh6h12kskmxj9n0).
// gx8xvgmph12krbjpg1n0 is the same walk skipping a subtree for a related reason.
//
// So the range is a KEEP and not a prune: every caller tests EndCommit for what the
// range MEANS, and this only stops the walk once it is past. What that costs is a
// cheap comparison per segment at or below `to`; what a caller does with the segments
// it keeps is unchanged, and that is where a read's work is.
func commitsUpTo(to *int64) func(LogSegment) int {
	if to == nil {
		return func(LogSegment) int { return 0 }
	}
	end := *to
	return func(v LogSegment) int {
		if v.StartCommit > end {
			return 1
		}
		return 0
	}
}

// inCommitRange reports whether a segment's own commit -- its EndCommit -- is in
// [from, to]. This is what a commit range means; commitsUpTo only says where the walk
// may stop looking.
func inCommitRange(from, to *int64) func(LogSegment) bool {
	return func(v LogSegment) bool {
		if from != nil && v.EndCommit < *from {
			return false
		}
		if to != nil && v.EndCommit > *to {
			return false
		}
		return true
	}
}

// ListRange returns the immediate child names at this index level.
// Returns the keys of the Children map.
// If scopeID is nil, returns only children with baseline segments.
// If scopeID is non-nil, returns children with baseline or matching scope segments.
func (i *Index) ListRange(from, to *int64, scopeID *string) []string {
	// LookupRange takes the child's lock itself, so nothing is held here -- and it was
	// taken twice on the same node before, which is a deadlock the moment a writer is
	// waiting between the two (Go's RWMutex does not admit a reader past a waiting
	// writer).
	snap := i.childrenOf()
	children := make([]string, 0, len(snap))
	for _, child := range snap {
		if len(child.index.LookupRange("", from, to, scopeID)) == 0 {
			continue
		}
		children = append(children, child.name) // already a valid kpath segment
	}
	// Sort children by kpath comparison (not string comparison)
	slices.SortFunc(children, func(a, b string) int {
		aKp, errA := kpath.Parse(a)
		bKp, errB := kpath.Parse(b)
		if errA != nil || errB != nil {
			return cmp.Compare(a, b) // Fallback
		}
		if aKp == nil && bKp == nil {
			return 0
		}
		if aKp == nil {
			return -1
		}
		if bKp == nil {
			return 1
		}
		return aKp.Compare(bKp)
	})
	return children
}

// DeleteScope removes all segments with the given scopeID from the index.
// Returns the number of segments removed.
func (i *Index) DeleteScope(scopeID string) int {
	i.Lock()
	defer i.Unlock()
	return i.deleteScopeLocked(scopeID)
}

// deleteScopeLocked removes scope segments without acquiring the lock (caller must hold it).
func (i *Index) deleteScopeLocked(scopeID string) int {
	count := 0

	// Collect segments to remove from this node
	var toRemove []LogSegment
	i.Commits.All(func(seg LogSegment) bool {
		if seg.ScopeID != nil && *seg.ScopeID == scopeID {
			toRemove = append(toRemove, seg)
		}
		return true
	})

	// Remove them
	for _, seg := range toRemove {
		if i.Commits.Remove(seg) {
			count++
		}
	}

	// Recurse into children
	for _, child := range i.Children {
		child.Lock()
		count += child.deleteScopeLocked(scopeID)
		child.Unlock()
	}

	return count
}
