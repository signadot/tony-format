package index

import (
	"math/rand"
	"testing"
)

// A range walk must find exactly what a full scan finds. It descends by the
// bounds a parent caches for each child, so a bound that is out of date is not a
// slower answer, it is a wrong one: the walk reads "this subtree ends before the
// range begins" and skips a subtree the element is in.
//
// That is what a leaf split did. The value goes into one of the halves AFTER
// merge has read their bounds, so when it is the new maximum the merged node
// says the subtree ends one element short, and every ancestor copies it. At 49
// ascending inserts -- the first size where a split leaves the new maximum in a
// fresh leaf -- Range for the last element found nothing while All found it.
//
// In logd that was a read at commit N missing the write committed at N whenever
// the newest snapshot was at N-1: the entry was in the log, indexed, and
// invisible (issue gx8xvgmph12krbjpg1n0).
func TestRangeAgreesWithScan(t *testing.T) {
	orders := []struct {
		name string
		seq  func(n int) []int64
	}{
		{"ascending", func(n int) []int64 {
			s := make([]int64, n)
			for i := range s {
				s[i] = int64(i + 1)
			}
			return s
		}},
		{"descending", func(n int) []int64 {
			s := make([]int64, n)
			for i := range s {
				s[i] = int64(n - i)
			}
			return s
		}},
		{"shuffled", func(n int) []int64 {
			s := make([]int64, n)
			for i := range s {
				s[i] = int64(i + 1)
			}
			rng := rand.New(rand.NewSource(int64(n)))
			rng.Shuffle(len(s), func(i, j int) { s[i], s[j] = s[j], s[i] })
			return s
		}},
	}
	// sizes around each split boundary, and a few larger
	sizes := []int{1, 2, 31, 32, 33, 47, 48, 49, 50, 63, 64, 65, 100, 257, 1024}

	for _, order := range orders {
		t.Run(order.name, func(t *testing.T) {
			for _, n := range sizes {
				idx := NewIndex("")
				for _, c := range order.seq(n) {
					idx.Commits.Insert(LogSegment{StartCommit: c - 1, StartTx: c, EndCommit: c})
				}
				for _, width := range []int64{0, 1, 7} {
					for c := int64(1); c <= int64(n); c++ {
						from, to := c, c+width
						var walked, scanned int
						idx.Commits.Range(func(LogSegment) bool { walked++; return true }, rangeFunc(&from, &to))
						idx.Commits.All(func(s LogSegment) bool {
							if s.EndCommit >= from && s.EndCommit <= to {
								scanned++
							}
							return true
						})
						if walked != scanned {
							t.Fatalf("n=%d range [%d,%d]: the walk found %d, a scan finds %d",
								n, from, to, walked, scanned)
						}
					}
				}
			}
		})
	}
}

// TestBoundsHoldAfterEveryInsert checks the invariant the walk rests on rather
// than one of its consequences: what a parent says about a child is what the
// child holds.
func TestBoundsHoldAfterEveryInsert(t *testing.T) {
	idx := NewIndex("")
	for c := int64(1); c <= 300; c++ {
		idx.Commits.Insert(LogSegment{StartCommit: c - 1, StartTx: c, EndCommit: c})
		checkBounds(t, idx.Commits.root, c)
	}
}

// checkBounds reports a node whose cached [min,max] disagree with its subtree.
func checkBounds(t *testing.T, n *node[LogSegment], after int64) (min, max LogSegment) {
	t.Helper()
	if n.isLeaf() {
		return n.D[0], n.D[len(n.D)-1]
	}
	var lo, hi LogSegment
	for i, c := range n.C {
		cLo, cHi := checkBounds(t, c, after)
		if i == 0 {
			lo = cLo
		}
		hi = cHi
	}
	if n.D[0].EndCommit != lo.EndCommit {
		t.Fatalf("after inserting %d: a node's cached min says %d, its subtree starts at %d",
			after, n.D[0].EndCommit, lo.EndCommit)
	}
	if n.D[1].EndCommit != hi.EndCommit {
		t.Fatalf("after inserting %d: a node's cached max says %d, its subtree ends at %d",
			after, n.D[1].EndCommit, hi.EndCommit)
	}
	return lo, hi
}
