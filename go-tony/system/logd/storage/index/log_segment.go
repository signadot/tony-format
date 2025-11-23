package index

import (
	"slices"
)

type LogSegment struct {
	StartCommit int64
	StartTx     int64
	EndCommit   int64
	EndTx       int64
	RelPath     string
}

func PointLogSegment(c, tx int64, p string) *LogSegment {
	return &LogSegment{
		StartCommit: c,
		EndCommit:   c,
		StartTx:     tx,
		EndTx:       tx,
		RelPath:     p,
	}
}

func (s *LogSegment) IsPoint() bool {
	return s.StartCommit == s.EndCommit
}

// SortLogSegments sorts a slice of LogSegment pointers by commit count, then tx.
func SortLogSegments(segments []*LogSegment) {
	// Use the existing LogSegCompare function
	slices.SortFunc(segments, func(a, b *LogSegment) int {
		return LogSegCompare(*a, *b)
	})
}
