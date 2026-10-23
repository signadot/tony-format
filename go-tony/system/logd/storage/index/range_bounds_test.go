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
					// Every 64th commit also opens a span that ends 100 commits on,
					// as a scope's statement does: a segment that starts well
					// before a range and ends inside it, which is why the lower
					// bound cannot come from the order (commitsUpTo).
					if c%64 == 0 {
						idx.Commits.Insert(LogSegment{StartCommit: c, StartTx: -1, EndCommit: c + 100})
					}
				}
				// The walk is checked as built, and again after the spans leave: the
				// greatest EndCommit a node caches must come down with them, or a
				// walk visits what it need not -- a cost, not an answer, and both
				// are asserted.
				for _, pass := range []string{"with spans", "spans removed"} {
					if pass == "spans removed" {
						for c := int64(64); c <= int64(n); c += 64 {
							if !idx.Commits.Remove(LogSegment{StartCommit: c, StartTx: -1, EndCommit: c + 100}) {
								t.Fatalf("n=%d: the span at %d was not there to remove", n, c)
							}
						}
					}
					for _, width := range []int64{0, 1, 7} {
						for c := int64(1); c <= int64(n); c++ {
							from, to := c, c+width
							var walked, scanned, visited int
							// The walk is bounded above by the order, below by the
							// tree's greatest EndCommit per subtree, and the range
							// itself is a keep, so what the three together answer is
							// what a scan answers.
							inRange := inCommitRange(&from, &to)
							upTo := commitsUpTo(&to)
							idx.Commits.RangeFrom(from, func(s LogSegment) bool {
								if inRange(s) {
									walked++
								}
								return true
							}, func(s LogSegment) int {
								visited++
								return upTo(s)
							})
							idx.Commits.All(func(s LogSegment) bool {
								if s.EndCommit >= from && s.EndCommit <= to {
									scanned++
								}
								return true
							})
							if walked != scanned {
								t.Fatalf("n=%d %s range [%d,%d]: the walk found %d, a scan finds %d",
									n, pass, from, to, walked, scanned)
							}
							// The walk is sublinear: it enters the subtrees that can
							// end in the range -- the ones at the head, plus one path
							// to each span -- not the whole history, which is what a
							// read paid at every level of the trie before
							// (baassnwth12krdjbvdn0). At 1024 a full walk compares
							// every element; a bounded one compares a few leaves.
							if n == 1024 && visited > n/4 {
								t.Fatalf("n=%d %s range [%d,%d]: the walk compared %d elements of %d",
									n, pass, from, to, visited, n)
							}
						}
					}
				}
			}
		})
	}
}

// TestBoundsHoldAfterEveryInsert checks the invariant the walk rests on rather
// than one of its consequences: what a parent says about a child is what the
// child holds -- its least and greatest element, and the greatest EndCommit in it,
// which is the lower bound's whole basis and is checked after every removal too.
func TestBoundsHoldAfterEveryInsert(t *testing.T) {
	idx := NewIndex("")
	for c := int64(1); c <= 300; c++ {
		idx.Commits.Insert(LogSegment{StartCommit: c - 1, StartTx: c, EndCommit: c})
		if c%50 == 0 {
			idx.Commits.Insert(LogSegment{StartCommit: c, StartTx: -1, EndCommit: c + 1000})
		}
		checkBounds(t, idx.Commits.root, c)
	}
	// Out from the middle, so leaves empty and parents lose children on the way.
	for c := int64(150); c <= 300; c++ {
		idx.Commits.Remove(LogSegment{StartCommit: c - 1, StartTx: c, EndCommit: c})
		if c%50 == 0 {
			idx.Commits.Remove(LogSegment{StartCommit: c, StartTx: -1, EndCommit: c + 1000})
		}
		checkBounds(t, idx.Commits.root, -c)
	}
}

// checkBounds reports a node whose cached [min,max] or greatest EndCommit disagree
// with its subtree. after names the step: a commit inserted, or negated, removed.
func checkBounds(t *testing.T, n *node[LogSegment], after int64) (min, max LogSegment, end int64) {
	t.Helper()
	if n.isLeaf() {
		end = n.leafHi()
		if n.hi != end {
			t.Fatalf("after %d: a leaf's cached greatest EndCommit says %d, its elements end at %d",
				after, n.hi, end)
		}
		return n.D[0], n.D[len(n.D)-1], end
	}
	var lo, hi LogSegment
	end = noEnd
	for i, c := range n.C {
		cLo, cHi, cEnd := checkBounds(t, c, after)
		if i == 0 {
			lo = cLo
		}
		hi = cHi
		if cEnd > end {
			end = cEnd
		}
	}
	if n.D[0].EndCommit != lo.EndCommit {
		t.Fatalf("after %d: a node's cached min says %d, its subtree starts at %d",
			after, n.D[0].EndCommit, lo.EndCommit)
	}
	if n.D[1].EndCommit != hi.EndCommit {
		t.Fatalf("after %d: a node's cached max says %d, its subtree ends at %d",
			after, n.D[1].EndCommit, hi.EndCommit)
	}
	if n.hi != end {
		t.Fatalf("after %d: a node's cached greatest EndCommit says %d, its subtree's is %d",
			after, n.hi, end)
	}
	return lo, hi, end
}
