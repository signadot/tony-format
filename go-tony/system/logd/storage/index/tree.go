package index

// Direction specifies the iteration direction for commits
type Direction int

const (
	// Up iterates commits in ascending order (smallest to largest)
	Up Direction = iota
	// Down iterates commits in descending order (largest to smallest)
	Down
)

type Tree[T any] struct {
	Less func(a, b T) bool
	root *node[T]
}

func NewTree[T any](less func(a, b T) bool) *Tree[T] {
	return &Tree[T]{
		Less: less,
		root: newLeaf[T](less),
	}
}

func (t *Tree[T]) Insert(v T) bool {
	alt, added := t.root.add(v, nil, 0)
	if alt != t.root {
		t.root = alt
	}
	return added
}

func (t *Tree[T]) Index(v T) int {
	return t.root.index(v)
}

func (t *Tree[T]) Remove(v T) bool {
	return t.root.remove(v)
}

// All applies f to all elements in T in ascending order
// until f returns false.  All returns whether or not f returns false
func (t *Tree[T]) All(f func(T) bool) bool {
	return t.root.all(f)
}

// Range applies f to all elements e such that r(e) == 0.
// r should return a negative integer for elements less than
// those such that r(e) == 0 and likewise a positive integer
// for those elements greater than those such that r(e) == 0
func (t *Tree[T]) Range(f func(T) bool, r func(T) int) bool {
	return t.root.rangeFunc(f, r)
}

// IterAscending returns an iterator positioned at the first element (minimum)
// Next() will advance through elements in ascending order
func (t *Tree[T]) IterAscending() *Iterator[T] {
	it := &Iterator[T]{
		tree:      t,
		stack:     make([]iteratorFrame[T], 0, 16),
		ascending: true,
		valid:     false,
	}
	it.seekToFirst()
	return it
}

// IterDescending returns an iterator positioned at the last element (maximum)
// Next() will advance through elements in descending order
func (t *Tree[T]) IterDescending() *Iterator[T] {
	it := &Iterator[T]{
		tree:      t,
		stack:     make([]iteratorFrame[T], 0, 16),
		ascending: false,
		valid:     false,
	}
	it.seekToLast()
	return it
}

// IterSeek positions iterator at the first element >= target (if ascending)
// or <= target (if descending). Next() will advance in the specified direction.
func (t *Tree[T]) IterSeek(target T, ascending bool) *Iterator[T] {
	it := &Iterator[T]{
		tree:      t,
		stack:     make([]iteratorFrame[T], 0, 16),
		ascending: ascending,
		valid:     false,
	}
	if ascending {
		it.seekToFirstGE(target)
	} else {
		it.seekToLastLE(target)
	}
	return it
}

// IterRange positions iterator at the first element matching the range predicate
// Next() will advance in the specified direction.
func (t *Tree[T]) IterRange(r func(T) int, ascending bool) *Iterator[T] {
	it := &Iterator[T]{
		tree:      t,
		stack:     make([]iteratorFrame[T], 0, 16),
		ascending: ascending,
		valid:     false,
	}
	if ascending {
		it.seekToRangeAscending(r)
	} else {
		it.seekToRangeDescending(r)
	}
	return it
}

// Commits returns a function that can be used with for-range to iterate
// elements in the specified direction.
func (t *Tree[T]) Commits(dir Direction) func(func(T) bool) {
	var iter *Iterator[T]
	if dir == Up {
		iter = t.IterAscending()
	} else {
		iter = t.IterDescending()
	}

	return func(yield func(T) bool) {
		if !iter.Valid() {
			return
		}
		if !yield(iter.Value()) {
			return
		}
		for iter.Next() {
			if !yield(iter.Value()) {
				return
			}
		}
	}
}
