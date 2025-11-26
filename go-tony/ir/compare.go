package ir

import (
	"cmp"
	"strings"
)

// Compare returns an integer comparing two nodes.
// The result will be 0 if a==b, -1 if a < b, and +1 if a > b.
func Compare(a, b *Node) int {
	if a == b {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}

	rankA := rank(a.Type)
	rankB := rank(b.Type)
	if rankA != rankB {
		return cmp.Compare(rankA, rankB)
	}

	switch a.Type {
	case NumberType:
		return compareNumbers(a, b)
	case StringType:
		return strings.Compare(a.String, b.String)
	case BoolType:
		if a.Bool == b.Bool {
			return 0
		}
		if !a.Bool {
			return -1
		}
		return 1
	case ArrayType:
		return compareArrays(a, b)
	case ObjectType:
		return compareObjects(a, b)
	case NullType, CommentType:
		return 0
	}
	return 0
}

// rank returns the sorting rank of a type.
// Order: Comment < Null < Number < String < Array < Object
func rank(t Type) int {
	switch t {
	case CommentType:
		return 0
	case NullType:
		return 1
	case BoolType:
		return 2
	case NumberType:
		return 3
	case StringType:
		return 4
	case ArrayType:
		return 5
	case ObjectType:
		return 6
	}
	return 100
}

func compareNumbers(a, b *Node) int {
	// Sub-rank: Int64 < Float64 < String
	subRankA := numberSubRank(a)
	subRankB := numberSubRank(b)
	if subRankA != subRankB {
		return cmp.Compare(subRankA, subRankB)
	}

	if a.Int64 != nil {
		return cmp.Compare(*a.Int64, *b.Int64)
	}
	if a.Float64 != nil {
		return cmp.Compare(*a.Float64, *b.Float64)
	}
	return strings.Compare(a.Number, b.Number)
}

func numberSubRank(n *Node) int {
	if n.Int64 != nil {
		return 0
	}
	if n.Float64 != nil {
		return 1
	}
	return 2
}

func compareArrays(a, b *Node) int {
	lenA := len(a.Values)
	lenB := len(b.Values)
	minLen := min(lenA, lenB)

	for i := 0; i < minLen; i++ {
		if c := Compare(a.Values[i], b.Values[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(lenA, lenB)
}

func compareObjects(a, b *Node) int {
	// Compare based on fields (keys) first, then values?
	// Usually objects are compared by size then content, or just content.
	// The requirement mentions "within object IntKeys < String+Merge keys".
	// This implies we are comparing keys when sorting map entries, but here we are comparing two Object nodes.
	// Assuming lexicographical comparison of fields for now.

	lenA := len(a.Fields)
	lenB := len(b.Fields)
	minLen := min(lenA, lenB)

	for i := 0; i < minLen; i++ {
		// Compare keys
		if c := Compare(a.Fields[i], b.Fields[i]); c != 0 {
			return c
		}
		// Compare values
		if c := Compare(a.Values[i], b.Values[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(lenA, lenB)
}
