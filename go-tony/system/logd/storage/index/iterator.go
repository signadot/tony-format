package index

import (
	"slices"
	"sort"
)

// Iterator provides caller-driven iteration over a Tree[T]
// Direction is fixed at creation (ascending or descending)
type Iterator[T any] struct {
	tree      *Tree[T]
	stack     []iteratorFrame[T]
	ascending bool // true for ascending, false for descending
	current   T
	valid     bool
}

type iteratorFrame[T any] struct {
	node  *node[T]
	index int // current position in node.D (leaf) or node.C (internal)
}

// seekToFirst positions the iterator at the first element (minimum)
func (it *Iterator[T]) seekToFirst() {
	if it.tree.root == nil || it.tree.root.N == 0 {
		it.valid = false
		return
	}
	it.stack = it.stack[:0]
	it.descendToFirst(it.tree.root)
}

// seekToLast positions the iterator at the last element (maximum)
func (it *Iterator[T]) seekToLast() {
	if it.tree.root == nil || it.tree.root.N == 0 {
		it.valid = false
		return
	}
	it.stack = it.stack[:0]
	it.descendToLast(it.tree.root)
}

// descendToFirst navigates to the first element in the subtree rooted at n
func (it *Iterator[T]) descendToFirst(n *node[T]) {
	it.stack = append(it.stack, iteratorFrame[T]{node: n, index: 0})
	if n.isLeaf() {
		if len(n.D) > 0 {
			it.current = n.D[0]
			it.valid = true
		} else {
			it.valid = false
		}
	} else {
		if len(n.C) > 0 {
			it.descendToFirst(n.C[0])
		} else {
			it.valid = false
		}
	}
}

// descendToLast navigates to the last element in the subtree rooted at n
func (it *Iterator[T]) descendToLast(n *node[T]) {
	if n.isLeaf() {
		idx := len(n.D) - 1
		it.stack = append(it.stack, iteratorFrame[T]{node: n, index: idx})
		if idx >= 0 {
			it.current = n.D[idx]
			it.valid = true
		} else {
			it.valid = false
		}
	} else {
		idx := len(n.C) - 1
		it.stack = append(it.stack, iteratorFrame[T]{node: n, index: idx})
		if idx >= 0 {
			it.descendToLast(n.C[idx])
		} else {
			it.valid = false
		}
	}
}

// Next advances the iterator to the next element in the iterator's direction
// For ascending iterators, moves to next larger element
// For descending iterators, moves to next smaller element
// Returns true if a valid element was found, false if iteration is complete
func (it *Iterator[T]) Next() bool {
	if !it.valid {
		return false
	}

	if it.ascending {
		return it.nextAscending()
	} else {
		return it.nextDescending()
	}
}

// nextAscending advances the iterator to the next element in ascending order
func (it *Iterator[T]) nextAscending() bool {
	if len(it.stack) == 0 {
		it.valid = false
		return false
	}

	frame := &it.stack[len(it.stack)-1]
	if frame.node.isLeaf() {
		frame.index++
		if frame.index < len(frame.node.D) {
			it.current = frame.node.D[frame.index]
			return true
		}
		// End of leaf, pop and continue
		it.stack = it.stack[:len(it.stack)-1]
		return it.nextAscending()
	} else {
		// Internal node: move to next child
		frame.index++
		if frame.index < len(frame.node.C) {
			it.descendToFirst(frame.node.C[frame.index])
			return true
		}
		// End of children, pop and continue
		it.stack = it.stack[:len(it.stack)-1]
		return it.nextAscending()
	}
}

// nextDescending advances the iterator to the next element in descending order
func (it *Iterator[T]) nextDescending() bool {
	if len(it.stack) == 0 {
		it.valid = false
		return false
	}

	frame := &it.stack[len(it.stack)-1]
	if frame.node.isLeaf() {
		frame.index--
		if frame.index >= 0 {
			it.current = frame.node.D[frame.index]
			return true
		}
		// Beginning of leaf, pop and continue
		it.stack = it.stack[:len(it.stack)-1]
		return it.nextDescending()
	} else {
		// Internal node: move to previous child
		frame.index--
		if frame.index >= 0 {
			it.descendToLast(frame.node.C[frame.index])
			return true
		}
		// Beginning of children, pop and continue
		it.stack = it.stack[:len(it.stack)-1]
		return it.nextDescending()
	}
}

// Value returns the current element
// Panics if iterator is not valid
func (it *Iterator[T]) Value() T {
	if !it.valid {
		panic("iterator is not valid")
	}
	return it.current
}

// Valid returns whether the iterator is positioned at a valid element
func (it *Iterator[T]) Valid() bool {
	return it.valid
}

// Seek positions the iterator at the first element >= target (if ascending)
// or <= target (if descending). Direction remains unchanged.
func (it *Iterator[T]) Seek(target T) bool {
	it.stack = it.stack[:0]
	it.valid = false
	if it.ascending {
		it.seekToFirstGE(target)
	} else {
		it.seekToLastLE(target)
	}
	return it.valid
}

// seekToFirstGE positions iterator at first element >= target
func (it *Iterator[T]) seekToFirstGE(target T) {
	if it.tree.root == nil || it.tree.root.N == 0 {
		it.valid = false
		return
	}
	it.stack = it.stack[:0]
	it.descendToFirstGE(it.tree.root, target)
}

// seekToLastLE positions iterator at last element <= target
func (it *Iterator[T]) seekToLastLE(target T) {
	if it.tree.root == nil || it.tree.root.N == 0 {
		it.valid = false
		return
	}
	it.stack = it.stack[:0]
	it.descendToLastLE(it.tree.root, target)
}

// descendToFirstGE navigates to first element >= target
func (it *Iterator[T]) descendToFirstGE(n *node[T], target T) {
	if n.isLeaf() {
		// Binary search for first element >= target
		idx, found := slices.BinarySearchFunc(n.D, target, n.cmpFunc())
		if !found && idx < len(n.D) {
			// idx points to insertion point (first >= target)
			it.stack = append(it.stack, iteratorFrame[T]{node: n, index: idx})
			it.current = n.D[idx]
			it.valid = true
		} else if found {
			it.stack = append(it.stack, iteratorFrame[T]{node: n, index: idx})
			it.current = n.D[idx]
			it.valid = true
		} else {
			// All elements < target
			it.valid = false
		}
		return
	}

	// Internal node: find appropriate child
	for i, c := range n.C {
		cMax := c.max()
		if !n.Less(cMax, target) {
			// target <= cMax, so target might be in this child
			it.stack = append(it.stack, iteratorFrame[T]{node: n, index: i})
			it.descendToFirstGE(c, target)
			return
		}
	}
	// All children have max < target
	it.valid = false
}

// descendToLastLE navigates to last element <= target
func (it *Iterator[T]) descendToLastLE(n *node[T], target T) {
	if n.isLeaf() {
		// Binary search for last element <= target
		idx, found := slices.BinarySearchFunc(n.D, target, n.cmpFunc())
		if found {
			it.stack = append(it.stack, iteratorFrame[T]{node: n, index: idx})
			it.current = n.D[idx]
			it.valid = true
		} else if idx > 0 {
			// idx points to insertion point, element before is <= target
			idx--
			it.stack = append(it.stack, iteratorFrame[T]{node: n, index: idx})
			it.current = n.D[idx]
			it.valid = true
		} else {
			// All elements > target
			it.valid = false
		}
		return
	}

	// Internal node: find appropriate child
	for i := len(n.C) - 1; i >= 0; i-- {
		c := n.C[i]
		cMin := c.min()
		if !n.Less(target, cMin) {
			// target >= cMin, so target might be in this child
			it.stack = append(it.stack, iteratorFrame[T]{node: n, index: i})
			it.descendToLastLE(c, target)
			return
		}
	}
	// All children have min > target
	it.valid = false
}

// seekToRangeAscending positions iterator at first element matching range predicate
func (it *Iterator[T]) seekToRangeAscending(r func(T) int) {
	if it.tree.root == nil || it.tree.root.N == 0 {
		it.valid = false
		return
	}
	it.stack = it.stack[:0]
	it.descendToRangeAscending(it.tree.root, r)
}

// seekToRangeDescending positions iterator at last element matching range predicate
func (it *Iterator[T]) seekToRangeDescending(r func(T) int) {
	if it.tree.root == nil || it.tree.root.N == 0 {
		it.valid = false
		return
	}
	it.stack = it.stack[:0]
	it.descendToRangeDescending(it.tree.root, r)
}

// descendToRangeAscending navigates to first element matching range predicate
func (it *Iterator[T]) descendToRangeAscending(n *node[T], r func(T) int) {
	if n.isLeaf() {
		idx, found := sort.Find(len(n.D), func(i int) int { return -r(n.D[i]) })
		if found {
			it.stack = append(it.stack, iteratorFrame[T]{node: n, index: idx})
			it.current = n.D[idx]
			it.valid = true
		} else {
			it.valid = false
		}
		return
	}

	for i, c := range n.C {
		if r(c.max()) < 0 {
			continue
		}
		if r(c.min()) > 0 {
			break
		}
		it.stack = append(it.stack, iteratorFrame[T]{node: n, index: i})
		it.descendToRangeAscending(c, r)
		return
	}
	it.valid = false
}

// descendToRangeDescending navigates to last element matching range predicate
func (it *Iterator[T]) descendToRangeDescending(n *node[T], r func(T) int) {
	if n.isLeaf() {
		idx, found := sort.Find(len(n.D), func(i int) int { return -r(n.D[i]) })
		if found {
			// For descending, we want the last matching element
			// Start from idx and find the last one matching
			for idx < len(n.D) && r(n.D[idx]) == 0 {
				idx++
			}
			if idx > 0 {
				idx--
				it.stack = append(it.stack, iteratorFrame[T]{node: n, index: idx})
				it.current = n.D[idx]
				it.valid = true
			} else {
				it.valid = false
			}
		} else {
			it.valid = false
		}
		return
	}

	for i := len(n.C) - 1; i >= 0; i-- {
		c := n.C[i]
		if r(c.min()) > 0 {
			continue
		}
		if r(c.max()) < 0 {
			break
		}
		it.stack = append(it.stack, iteratorFrame[T]{node: n, index: i})
		it.descendToRangeDescending(c, r)
		return
	}
	it.valid = false
}
