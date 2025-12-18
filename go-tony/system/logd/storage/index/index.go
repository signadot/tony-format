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
				return a.KindedPath < b.KindedPath
			}
			// Handle nil cases
			if aKp == nil && bKp == nil {
				return false
			}
			if aKp == nil {
				return true // Empty < non-empty
			}
			if bKp == nil {
				return false // Non-empty > empty
			}
			return aKp.Compare(bKp) < 0
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

func (i *Index) LookupRange(kp string, from, to *int64) []LogSegment {
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
	// Split kpath to navigate hierarchy
	firstSegment, restPath := kpath.Split(kp)
	c := i.Children[firstSegment]
	if c == nil {
		return res
	}
	// Recursive call - child index has its own lock, so this is safe
	cRes := c.LookupRange(restPath, from, to)
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

// LookupWithin finds all segments at the given kpath where the commit is within
// the segment's commit range (StartCommit <= commit <= EndCommit).
// Returns ancestors and exact matches, just like LookupRange.
func (i *Index) LookupWithin(kp string, commit int64) []LogSegment {
	i.RLock()
	defer i.RUnlock()
	res := []LogSegment{}
	i.Commits.Range(func(c LogSegment) bool {
		if c.StartCommit <= commit && commit <= c.EndCommit {
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
	cRes := c.LookupWithin(restPath, commit)
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
		return cmp.Compare(a.KindedPath, b.KindedPath)
	}
	// Handle nil cases (empty paths)
	if aKp == nil && bKp == nil {
		return 0
	}
	if aKp == nil {
		return -1 // Empty path < non-empty
	}
	if bKp == nil {
		return 1 // Non-empty > empty
	}
	return aKp.Compare(bKp)
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
func (i *Index) ListRange(from, to *int64) []string {
	i.RLock()
	defer i.RUnlock()

	children := make([]string, 0, len(i.Children))
	for pathKey, ci := range i.Children {
		ci.RLock()
		segs := ci.LookupRange("", from, to)
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
