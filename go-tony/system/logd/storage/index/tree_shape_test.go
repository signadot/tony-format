package index

import (
	"strconv"
	"testing"
)

// treeDepth is the longest root-to-leaf path.
func treeDepth[T any](n *node[T]) int {
	if n == nil {
		return 0
	}
	if n.isLeaf() {
		return 1
	}
	best := 0
	for _, c := range n.C {
		if d := treeDepth(c); d > best {
			best = d
		}
	}
	return best + 1
}

// The commit tree must stay SHALLOW. It did not: a split hung its two halves off the
// parent as a new level rather than handing them up to sit side by side, so every split
// made the tree one deeper and it degenerated into a linked list -- depth 3124 at fifty
// thousand entries, every interior node holding exactly two children. An insert walked
// that chain, so indexing a patch cost O(commits) and a write got slower for as long as
// the store lived: 58µs per patch at 5k commits, 3.8ms at 100k
// (kds4sx3bh12krdrkghn0).
//
// Depth is asserted rather than time: it is the property, and it does not depend on the
// machine.
func TestTreeStaysShallow(t *testing.T) {
	for _, n := range []int{1000, 10000, 50000} {
		tr := NewTree(func(a, b int) bool { return a < b })
		for i := 0; i < n; i++ {
			tr.Insert(i)
		}
		depth := treeDepth(tr.root)
		// log32(50000) is under 4; 8 leaves room for a partly filled tree and none
		// for a chain.
		if depth > 8 {
			t.Errorf("%d entries: depth %d -- the tree is growing a level per split", n, depth)
		}
		t.Logf("%6d entries: depth %d", n, depth)
	}
}

// Re-inserting a value the tree already holds must change nothing. It used to report
// overflow before checking for the duplicate, so a re-insert into a FULL leaf split it
// -- and the caller, told nothing was added, returned without taking the two halves.
// The upper half, and everything in it, was dropped: entries the tree had accepted
// stopped being findable. The index re-adds segments whenever it is rebuilt from the
// logs, so this was reachable on any restart.
func TestReinsertIntoAFullLeafKeepsEverything(t *testing.T) {
	tr := NewTree(func(a, b int) bool { return a < b })
	const n = maxLeaf * 4
	for i := 0; i < n; i++ {
		if !tr.Insert(i) {
			t.Fatalf("insert %d: not added", i)
		}
	}
	for i := 0; i < n; i++ {
		if tr.Insert(i) {
			t.Fatalf("re-inserting %d reported it as new", i)
		}
	}

	seen := 0
	tr.All(func(int) bool { seen++; return true })
	if seen != n {
		t.Errorf("the tree holds %d of %d values after re-inserting them all", seen, n)
	}
	for i := 0; i < n; i++ {
		if tr.Index(i) == -1 {
			t.Fatalf("value %d is no longer findable", i)
		}
	}
	if tr.root.N != n {
		t.Errorf("root count is %d, want %d", tr.root.N, n)
	}
}

// The same, through the index a store actually builds: a patch indexed at every path
// inside it, twice, as a rebuild does.
func TestIndexRebuildIsIdempotent(t *testing.T) {
	idx := NewIndex("")
	add := func(commit int64) {
		for _, kp := range []string{"", "verse", "verse.entities", "verse.entities.e" + strconv.FormatInt(commit%50, 10)} {
			idx.Add(&LogSegment{
				StartCommit: commit - 1, EndCommit: commit, KindedPath: kp,
				LogFile: "A", LogPosition: commit * 512,
			})
		}
	}
	for c := int64(1); c <= 500; c++ {
		add(c)
	}
	first := len(idx.LookupRangeAll("", nil, nil))
	for c := int64(1); c <= 500; c++ {
		add(c) // the rebuild
	}
	if again := len(idx.LookupRangeAll("", nil, nil)); again != first {
		t.Errorf("a rebuild changed the index: %d segments, was %d", again, first)
	}
}
