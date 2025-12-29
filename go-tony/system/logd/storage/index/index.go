package index

import (
	"cmp"
	"math"
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

// LookupRange finds segments in the given commit range.
// If scopeID is nil, returns only baseline segments.
// If scopeID is non-nil, returns baseline + matching scope segments.
func (i *Index) LookupRange(kp string, from, to *int64, scopeID *string) []LogSegment {
	i.RLock()
	defer i.RUnlock()
	res := []LogSegment{}
	i.Commits.Range(func(c LogSegment) bool {
		if matchesScope(c.ScopeID, scopeID) {
			res = append(res, c)
		}
		return true
	}, rangeFunc(from, to))
	if kp == "" {
		slices.SortFunc(res, LogSegCompare)
		return res
	}
	// Split kpath to navigate hierarchy
	firstSegment, restPath := kpath.Split(kp)
	c := i.Children[firstSegment]
	if c == nil {
		return res
	}
	// Recursive call - child index has its own lock, so this is safe
	cRes := c.LookupRange(restPath, from, to, scopeID)
	// Reconstruct full kpath for results
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
	i.Commits.Range(func(c LogSegment) bool {
		res = append(res, c)
		return true
	}, rangeFunc(from, to))
	if kp == "" {
		slices.SortFunc(res, LogSegCompare)
		return res
	}
	firstSegment, restPath := kpath.Split(kp)
	c := i.Children[firstSegment]
	if c == nil {
		return res
	}
	cRes := c.LookupRangeAll(restPath, from, to)
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

// LookupWithin finds all segments at the given kpath where the commit is within
// the segment's commit range (StartCommit <= commit <= EndCommit).
// Returns ancestors and exact matches, just like LookupRange.
// If scopeID is nil, returns only baseline segments.
// If scopeID is non-nil, returns baseline + matching scope segments.
func (i *Index) LookupWithin(kp string, commit int64, scopeID *string) []LogSegment {
	i.RLock()
	defer i.RUnlock()
	res := []LogSegment{}
	i.Commits.Range(func(c LogSegment) bool {
		if c.StartCommit <= commit && commit <= c.EndCommit && matchesScope(c.ScopeID, scopeID) {
			res = append(res, c)
		}
		return true
	}, withinFunc(commit))
	if kp == "" {
		slices.SortFunc(res, LogSegCompare)
		return res
	}
	firstSegment, restPath := kpath.Split(kp)
	c := i.Children[firstSegment]
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
func withinFunc(commit int64) func(LogSegment) int {
	return func(v LogSegment) int {
		if v.EndCommit < commit {
			return -1
		}
		if v.StartCommit > commit {
			return 1
		}
		return 0
	}
}

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

func rangeFunc(from, to *int64) func(LogSegment) int {
	start := int64(-1)
	if from != nil {
		start = *from
	}
	end := int64(math.MaxInt64)
	if to != nil {
		end = *to
	}
	return func(v LogSegment) int {
		// For patches: EndCommit is the commit of the patch
		// For snapshots: StartCommit == EndCommit
		// We want segments where the patch commit (EndCommit) is in [start, end]
		if v.EndCommit < start {
			return -1
		}
		if v.EndCommit > end {
			return 1
		}
		return 0
	}
}

// ListRange returns the immediate child names at this index level.
// Returns the keys of the Children map.
// If scopeID is nil, returns only children with baseline segments.
// If scopeID is non-nil, returns children with baseline or matching scope segments.
func (i *Index) ListRange(from, to *int64, scopeID *string) []string {
	i.RLock()
	defer i.RUnlock()

	children := make([]string, 0, len(i.Children))
	for pathKey, ci := range i.Children {
		ci.RLock()
		segs := ci.LookupRange("", from, to, scopeID)
		ci.RUnlock()
		if len(segs) == 0 {
			continue
		}
		children = append(children, pathKey) // pathKey is already a valid kpath segment
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
