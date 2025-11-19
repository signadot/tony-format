package index

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
