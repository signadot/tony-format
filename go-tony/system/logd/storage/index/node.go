package index

import (
	"slices"
	"sort"
)

type node[T any] struct {
	N    int
	D    []T
	C    []*node[T]
	Less func(a, b T) bool
}

func newLeaf[T any](less func(a, b T) bool) *node[T] {
	return &node[T]{
		D:    make([]T, 0, 32),
		Less: less,
	}
}

func newParent[T any](less func(a, b T) bool) *node[T] {
	return &node[T]{
		C:    make([]*node[T], 0, 32),
		D:    make([]T, 2),
		Less: less,
	}
}

func (n *node[T]) split() *node[T] {
	if n.isLeaf() {
		sort.Slice(n.D, func(i, j int) bool {
			return n.Less(n.D[i], n.D[j])
		})
		mid := len(n.D) / 2
		res := newLeaf[T](n.Less)
		res.D = append(res.D, n.D[mid:]...)
		n.D = n.D[:mid]
		n.N = mid
		res.N = len(res.D)
		return res
	}
	res := newParent[T](n.Less)
	mid := len(n.C) / 2
	N := len(n.C)
	for i := mid; i < N; i++ {
		c := n.C[i]
		res.N += c.N
		res.C = append(res.C, c)
		n.C[i] = nil
	}
	n.C = n.C[:mid]
	// Recompute N for n
	n.N = 0
	for _, c := range n.C {
		n.N += c.N
	}

	// Update D (min/max) for n and res
	if len(n.C) > 0 {
		n.updateBounds()
	}

	if len(res.C) > 0 {
		res.updateBounds()
	}

	return res
}

func merge[T any](a, b *node[T]) *node[T] {
	res := newParent[T](a.Less)
	if a.isLeaf() {
		res.D[0] = a.D[0]
		res.D[1] = b.D[len(b.D)-1]
		res.N = a.N + b.N
	} else {
		res.D[0] = a.D[0]
		res.D[1] = b.D[1]
		res.N = a.N + b.N
	}

	res.C = append(res.C, a, b)
	return res
}

func (n *node[T]) isLeaf() bool {
	return n.C == nil
}

func (n *node[T]) leafIndex(v T) int {
	i, found := slices.BinarySearchFunc(n.D, v, n.cmpFunc())
	if found {
		return i
	}
	return -1
}

func (n *node[T]) add(v T, p *node[T], at int) (*node[T], bool) {
	if n.isLeaf() {
		add, over := n.leafAdd(v)
		if over {
			alt := n.split()
			repl := merge(n, alt)
			res := false
			if n.Less(v, alt.D[0]) {
				res, _ = n.leafAdd(v)
			} else {
				res, _ = alt.leafAdd(v)
			}
			repl.N = n.N + alt.N

			if p != nil {
				p.C[at] = repl
			}
			if p == nil {
				return repl, res
			}
			p.C[at] = repl
			return n, res
		}
		return n, add
	}

	for i, c := range n.C {
		isLast := i == len(n.C)-1

		var cMax T
		if c.isLeaf() {
			if len(c.D) > 0 {
				cMax = c.D[len(c.D)-1]
			}
		} else {
			cMax = c.D[1]
		}

		if isLast || !n.Less(cMax, v) {
			repl, added := c.add(v, n, i)
			if !added {
				return n, false
			}
			n.N++

			if repl != c {
				n.C[i] = repl.C[0]
				n.C = slices.Insert(n.C, i+1, repl.C[1])
			}

			// Update n.D (min/max) after potential structure changes
			if len(n.C) > 0 {
				n.updateBounds()
			}

			if len(n.C) > cap(n.C) {
				alt := n.split()
				replParent := merge(n, alt)
				return replParent, true
			}
			return n, true
		}
	}
	return n, false
}

func (n *node[T]) index(v T) int {
	if n.isLeaf() {
		return n.leafIndex(v)
	}
	if n.Less(v, n.D[0]) {
		return -1
	}
	if n.Less(n.D[1], v) {
		return -1
	}
	off := 0
	for i := 0; i < len(n.C); i++ {
		c := n.C[i]
		if c.isLeaf() {
			cIndex := c.leafIndex(v)
			if cIndex != -1 {
				return off + cIndex
			}
			off += c.N
			continue
		}
		if !n.Less(v, c.D[len(c.D)-1]) {
			if !n.Less(c.D[len(c.D)-1], v) {
				// v == max, in this child
			} else {
				// v > max, skip
				off += c.N
				continue
			}
		}
		cIndex := c.index(v)
		if cIndex != -1 {
			return off + cIndex
		}
		off += c.N
	}
	return -1
}

func (n *node[T]) remove(v T) bool {
	if n.isLeaf() {
		return n.leafRemove(v)
	}
	rmIndex := -1
	for i, c := range n.C {
		cMax := c.max()
		if c.Less(cMax, v) {
			continue
		}
		removed := c.remove(v)
		if !removed {
			return false
		}
		n.N--
		if c.N != 0 {
			// then n.N > 0
			n.updateBounds()
			return true
		}
		rmIndex = i
		break
	}
	if rmIndex == -1 {
		// outside bounds
		return false
	}
	n.C = slices.Delete(n.C, rmIndex, rmIndex+1)
	if n.N != 0 {
		n.updateBounds()
	}
	return false
}

func (n *node[T]) all(f func(T) bool) bool {
	if n.isLeaf() {
		for _, elt := range n.D {
			if !f(elt) {
				return false
			}
		}
		return true
	}
	for _, c := range n.C {
		if !c.all(f) {
			return false
		}
	}
	return true
}

func (n *node[T]) rangeFunc(f func(T) bool, r func(a T) int) bool {
	if n.isLeaf() {
		return n.leafRange(f, r)
	}
	return n.parentRange(f, r)
}

func (n *node[T]) leafRange(f func(T) bool, r func(a T) int) bool {
	i, found := sort.Find(len(n.D), func(i int) int { return -r(n.D[i]) })
	if !found {
		return true
	}
	for i < len(n.D) {
		elt := n.D[i]
		if r(elt) != 0 {
			return true
		}
		if !f(elt) {
			return false
		}
		i++
	}
	return true
}

func (n *node[T]) parentRange(f func(T) bool, r func(a T) int) bool {
	for _, c := range n.C {
		if r(c.max()) < 0 {
			continue
		}
		if r(c.min()) > 0 {
			return true
		}
		if !c.rangeFunc(f, r) {
			return false
		}
	}
	return true
}

func (n *node[T]) leafRemove(v T) bool {
	if len(n.D) == 0 {
		return false
	}
	index, found := slices.BinarySearchFunc(n.D, v, n.cmpFunc())
	if !found {
		return false
	}
	n.D = slices.Delete(n.D, index, index+1)
	n.N--
	return true
}

func (n *node[T]) leafAdd(v T) (added, overflow bool) {
	if len(n.D) == cap(n.D) {
		return false, true
	}
	index, found := slices.BinarySearchFunc(n.D, v, n.cmpFunc())
	if found {
		return false, false
	}
	n.D = slices.Insert(n.D, index, v)
	n.N++
	return true, false
}

func (n *node[T]) cmpFunc() func(a, b T) int {
	return func(a, b T) int {
		if n.Less(a, b) {
			return -1
		}
		if n.Less(b, a) {
			return 1
		}
		return 0
	}
}

// pre: nonempty
func (n *node[T]) min() T {
	if n.isLeaf() {
		return n.D[0]
	}
	return n.D[0]
}

// pre: nonempty
func (n *node[T]) max() T {
	if n.isLeaf() {
		return n.D[len(n.D)-1]
	}
	return n.D[1]
}

func (n *node[T]) updateBounds() {
	if n.isLeaf() {
		return
	}
	if n.N == 0 {
		return
	}
	n.D[0] = n.C[0].min()
	n.D[1] = n.C[len(n.C)-1].max()

}
