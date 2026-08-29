package index

import (
	"testing"
)

func seg(start, end, tx int64) *LogSegment {
	return &LogSegment{StartCommit: start, EndCommit: end, StartTx: tx, EndTx: tx}
}

// A commit range is asked in terms of EndCommit -- the commit of the patch -- and the
// tree is ordered by StartCommit. The segments in a range are therefore NOT a
// contiguous run, and a walk which prunes on EndCommit stops in the middle of one.
//
// This is the arrangement an overlay and the writes around it produce: the overlay
// carries a sentinel tx of -1, so it sorts before the scope's own write at the same
// StartCommit, and a snapshot segment sits between them with EndCommit one lower.
//
//	[2,3]tx3  [3,4]tx-1  [3,3]tx0  [3,4]tx4  [4,5]tx5
//	                 ^ found     ^ stopped here      ^ never reached
//
// A lookup for EndCommit in [4,5] answered [3,4] alone, and the scope read built on it
// lost the write that followed an overlay -- which reappeared as soon as any later
// commit landed (tmwq9mh6h12kskmxj9n0).
func TestACommitRangeIsNotAContiguousRun(t *testing.T) {
	idx := NewIndex("")
	for _, s := range []*LogSegment{
		seg(0, 1, 1),
		seg(1, 2, 2),
		seg(2, 3, -1),
		seg(2, 3, 3),
		seg(3, 4, -1),
		seg(3, 3, 0),
		seg(3, 4, 4),
		seg(4, 4, 0),
		seg(4, 5, 5),
	} {
		idx.Add(s)
	}

	tests := []struct {
		name     string
		from, to *int64
		want     []int64 // the EndCommits expected, in any order
	}{
		{"the range the overlay read asks for", ptr(4), ptr(5), []int64{4, 4, 4, 5}},
		{"one commit", ptr(5), ptr(5), []int64{5}},
		{"a range no segment ends in", ptr(6), ptr(9), nil},
		{"from the start", nil, ptr(5), []int64{1, 2, 3, 3, 3, 4, 4, 4, 5}},
		{"unbounded", nil, nil, []int64{1, 2, 3, 3, 3, 4, 4, 4, 5}},
		{"below every segment", ptr(0), ptr(0), nil},
		{"across the whole thing", ptr(1), ptr(5), []int64{1, 2, 3, 3, 3, 4, 4, 4, 5}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ends(idx.LookupRange("", test.from, test.to, nil))
			if !sameMultiset(got, test.want) {
				t.Errorf("LookupRange gave EndCommits %v, want %v", got, test.want)
			}
			// The bounded answer must be exactly the unbounded one, filtered. That is
			// the whole contract, and it is what a bound may not change.
			var byHand []int64
			for _, e := range ends(idx.LookupRange("", nil, nil, nil)) {
				if test.from != nil && e < *test.from {
					continue
				}
				if test.to != nil && e > *test.to {
					continue
				}
				byHand = append(byHand, e)
			}
			if !sameMultiset(got, byHand) {
				t.Errorf("bounded gave %v; the unbounded answer filtered by hand is %v",
					got, byHand)
			}
		})
	}
}

// LookupWithin asks a different question -- which segments SPAN a commit -- of the same
// walk, and pruned on the same key it may not prune on.
func TestLookupWithinFindsEverySegmentSpanningTheCommit(t *testing.T) {
	idx := NewIndex("")
	for _, s := range []*LogSegment{
		seg(2, 3, -1),
		seg(3, 4, -1),
		seg(3, 3, 0),
		seg(3, 4, 4),
		seg(4, 5, 5),
	} {
		idx.Add(s)
	}
	got := len(idx.LookupWithin("", 4, nil))
	// [3,4]tx-1, [3,4]tx4 and [4,5]tx5 all span commit 4.
	if got != 3 {
		t.Errorf("LookupWithin(4) found %d segments, want 3", got)
	}
}

func ptr(v int64) *int64 { return &v }

func ends(segs []LogSegment) []int64 {
	var out []int64
	for _, s := range segs {
		out = append(out, s.EndCommit)
	}
	return out
}

func sameMultiset(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	count := map[int64]int{}
	for _, v := range a {
		count[v]++
	}
	for _, v := range b {
		count[v]--
		if count[v] < 0 {
			return false
		}
	}
	return true
}
