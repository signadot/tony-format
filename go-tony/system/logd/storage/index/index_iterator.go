package index

import (
	"math"

	"github.com/signadot/tony-format/go-tony/ir"
)

// IndexIterator provides hierarchical navigation and iteration over an Index
type IndexIterator struct {
	root    *Index
	path    []string // Path from root to current index
	current *Index   // Current index being iterated
	valid   bool     // Whether iterator is valid
}

// IterAtPath creates an iterator positioned at the specified path
// Path is relative to root (e.g., "foo.bar" or "" for root)
// If path doesn't exist, iterator is invalid
// Caller must hold RLock on root index
func (i *Index) IterAtPath(kpath string) *IndexIterator {
	it := &IndexIterator{
		root: i,
	}

	if kpath == "" {
		it.path = []string{}
		it.current = i
		it.valid = true
		return it
	}

	segments := ir.SplitAll(kpath)
	it.path = make([]string, 0, len(segments))
	current := i

	for _, segment := range segments {
		current.RLock()
		child := current.Children[segment]
		current.RUnlock()
		if child == nil {
			it.valid = false
			return it
		}
		it.path = append(it.path, segment)
		current = child
	}

	it.current = current
	it.valid = true
	return it
}

// Path returns the current path in the hierarchy (e.g., "foo.bar" or "" for root)
// Uses kpath-aware joining to properly reconstruct the path.
// ir.Join() joins a single segment (prefix) with a path (suffix), so we build from right to left.
func (it *IndexIterator) Path() string {
	if len(it.path) == 0 {
		return ""
	}
	if len(it.path) == 1 {
		return it.path[0]
	}
	// Build path from right to left: join each segment with the accumulated path
	// Start with the last segment, then join each previous segment
	result := it.path[len(it.path)-1]
	for i := len(it.path) - 2; i >= 0; i-- {
		result = ir.Join(it.path[i], result)
	}
	return result
}

// Depth returns the depth in the hierarchy (0 for root)
func (it *IndexIterator) Depth() int {
	return len(it.path)
}

// Valid returns whether the iterator is positioned at a valid index
func (it *IndexIterator) Valid() bool {
	return it.valid && it.current != nil
}

// Down navigates into a child index by path key (kpath segment)
// Returns true if child exists and navigation succeeded
// Caller must hold RLock on current index
func (it *IndexIterator) Down(pathKey string) bool {
	if it.current == nil {
		return false
	}

	it.current.RLock()
	child := it.current.Children[pathKey]
	it.current.RUnlock()

	if child == nil {
		return false
	}

	// Navigate into child
	it.path = append(it.path, pathKey)
	it.current = child
	it.valid = true
	return true
}

// Up navigates to the parent index
// Returns true if parent exists (false if already at root)
func (it *IndexIterator) Up() bool {
	if len(it.path) == 0 {
		return false // Already at root
	}

	// Navigate back to parent
	it.path = it.path[:len(it.path)-1]

	// Find parent by traversing from root
	it.current = it.root
	for _, segment := range it.path {
		it.current.RLock()
		child := it.current.Children[segment]
		it.current.RUnlock()
		if child == nil {
			it.valid = false
			return false
		}
		it.current = child
	}

	it.valid = true
	return true
}

// ToPath navigates to the specified path from root
// Returns true if path exists
// Caller must hold RLock on root index
func (it *IndexIterator) ToPath(kpath string) bool {
	if kpath == "" {
		it.path = []string{}
		it.current = it.root
		it.valid = true
		return true
	}

	segments := ir.SplitAll(kpath)
	newPath := make([]string, 0, len(segments))
	current := it.root

	for _, segment := range segments {
		current.RLock()
		child := current.Children[segment]
		current.RUnlock()
		if child == nil {
			return false
		}
		newPath = append(newPath, segment)
		current = child
	}

	it.path = newPath
	it.current = current
	it.valid = true
	return true
}

// Commits returns a function that can be used with for-range to iterate
// commits at the current level in the specified direction.
// Caller must hold RLock on current index.
func (it *IndexIterator) Commits(dir Direction) func(func(LogSegment) bool) {
	if it.current == nil {
		return func(func(LogSegment) bool) {}
	}

	return it.current.Commits.Commits(dir)
}

// CommitsAt returns a function that can be used with for-range to iterate
// commits at the current level starting from the specified commit in the specified direction.
// For dir=Down, seeks to the first segment <= commit and iterates downward (larger to smaller commits).
// For dir=Up, seeks to the first segment >= commit and iterates upward (smaller to larger commits).
// Caller must hold RLock on current index.
func (it *IndexIterator) CommitsAt(commit int64, dir Direction) func(func(LogSegment) bool) {
	if it.current == nil {
		return func(func(LogSegment) bool) {}
	}

	// Create a target LogSegment for seeking
	// For descending (Down): use maximum values to find last segment <= commit
	// For ascending (Up): use minimum values to find first segment >= commit
	var target LogSegment
	if dir == Down {
		// For descending, we want the last segment <= commit
		// Use maximum values for StartTx and KindedPath to ensure we find the right position
		target = LogSegment{
			StartCommit: commit,
			StartTx:     math.MaxInt64,
			EndCommit:   commit,
			EndTx:       math.MaxInt64,
			KindedPath:  "\xff\xff\xff\xff", // Maximum string value
		}
	} else {
		// For ascending, we want the first segment >= commit
		// Use minimum values for StartTx and KindedPath
		target = LogSegment{
			StartCommit: commit,
			StartTx:     0,
			EndCommit:   commit,
			EndTx:       0,
			KindedPath:  "",
		}
	}

	// For descending (Down), seek to last segment <= target (ascending=false)
	// For ascending (Up), seek to first segment >= target (ascending=true)
	ascending := dir == Up
	iter := it.current.Commits.IterSeek(target, ascending)

	return func(yield func(LogSegment) bool) {
		if !iter.Valid() {
			return
		}
		
		// Check if the first segment matches our criteria
		seg := iter.Value()
		if dir == Down {
			// For descending, only yield if segment.StartCommit <= commit
			if seg.StartCommit > commit {
				return
			}
		} else {
			// For ascending, only yield if segment.StartCommit >= commit
			if seg.StartCommit < commit {
				return
			}
		}
		
		if !yield(seg) {
			return
		}
		
		// Continue iterating in the specified direction
		for iter.Next() {
			seg := iter.Value()
			// For descending, stop if we've gone past commit
			if dir == Down && seg.StartCommit > commit {
				break
			}
			// For ascending, we continue upward (no upper bound check needed)
			if !yield(seg) {
				return
			}
		}
	}
}
