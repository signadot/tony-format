package index

import (
	"slices"

	"github.com/signadot/tony-format/go-tony/gomap"
)

//tony:schemagen=log-segment
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

// IsPoint returns true if and only if this log segment represents
// a diff from the preceding commit.
func (s *LogSegment) IsPoint() bool {
	return s.StartCommit == s.EndCommit && s.StartTx == s.EndTx
}

// AsPending returns a copy of the segment with commit counts zeroed (for pending files).
func (s *LogSegment) AsPending() *LogSegment {
	return &LogSegment{
		StartCommit: 0,
		EndCommit:   0,
		StartTx:     s.StartTx,
		EndTx:       s.EndTx,
		RelPath:     s.RelPath,
	}
}

// WithCommit returns a copy of the segment with the commit count set.
// For point segments: sets both Start and End to commitCount.
// For compacted segments: keeps StartCommit, sets EndCommit to commitCount.
func (s *LogSegment) WithCommit(commitCount int64) *LogSegment {
	result := *s
	if s.IsPoint() {
		result.StartCommit = commitCount
		result.EndCommit = commitCount
	} else {
		// Compacted: keep StartCommit, update EndCommit
		result.EndCommit = commitCount
	}
	return &result
}

func (s *LogSegment) String() string {
	as, _ := gomap.ToString(s, gomap.EncodeWire(true))
	return as
}

// SortLogSegments sorts a slice of LogSegment pointers by commit count, then tx.
func SortLogSegments(segments []*LogSegment) {
	// Use the existing LogSegCompare function
	slices.SortFunc(segments, func(a, b *LogSegment) int {
		return LogSegCompare(*a, *b)
	})
}

func WithinCommitRange(a, b *LogSegment) bool {
	if a.StartCommit < b.StartCommit {
		return false
	}
	if a.EndCommit > b.EndCommit {
		return false
	}
	return true
}
