package storage

import (
	"fmt"
	"io"
	"iter"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// DeltaCursor is a walk over the commits that changed a path, in commit order, one entry
// in hand at a time. A replay's peak is one entry rather than the range: collecting the
// range first cost about 65 KB per commit, so a 20k-commit catch-up moved 1.3 GB through
// the heap and a whole-history request had the pod evicted four times in fifteen minutes
// (89my9f0kh12ksqknjhn0).
type DeltaCursor interface {
	Next() (*CommitNotification, error) // io.EOF at the end
	Close() error
}

// Deltas opens a cursor over every commit in [from, to] that can reach kp, in the view
// scopeID names. Each is delivered in the client's vocabulary, exactly as the live
// notification for the same commit was: a copy of the stored entry with its keyed arrays
// raised (raise.go), so a watch that replays and a watch that follows live see the same
// bytes.
//
// It refuses a range that starts at or below the replay floor rather than answering the
// subset that survives compaction: the caller asked for every change in the range and
// cannot be given it, and a short answer is indistinguishable from a quiet period.
func (s *Storage) Deltas(from, to int64, scopeID *string, kp string) (DeltaCursor, error) {
	if floor := s.replayFloor.Load(); from <= floor {
		return nil, fmt.Errorf("%w: range starts at %d, exact from %d", ErrReplayCompacted, from, floor+1)
	}
	next, stop := iter.Pull(s.index.Segments(kp, &from, &to, scopeID))
	return &deltaCursor{s: s, next: next, stop: stop}, nil
}

type deltaCursor struct {
	s    *Storage
	next func() (index.LogSegment, bool)
	stop func()
	last int64
	have bool
}

func (d *deltaCursor) Next() (*CommitNotification, error) {
	for {
		seg, ok := d.next()
		if !ok {
			return nil, io.EOF
		}
		if seg.StartCommit == seg.EndCommit {
			continue // a snapshot
		}
		// One entry is indexed at every level it passes through; the copies share the
		// commit and arrive together.
		if d.have && seg.EndCommit == d.last {
			continue
		}
		d.last, d.have = seg.EndCommit, true
		entry, err := d.s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
		if err != nil {
			return nil, fmt.Errorf("failed to read entry at %s:%d: %w", seg.LogFile, seg.LogPosition, err)
		}
		if entry.Patch == nil {
			continue
		}
		return &CommitNotification{
			Commit:    entry.Commit,
			Timestamp: entry.Timestamp,
			Patch:     d.s.raiseDelta(entry.ScopeID, deliverable(entry.Patch), entry.Commit),
			ScopeID:   entry.ScopeID,
		}, nil
	}
}

func (d *deltaCursor) Close() error {
	d.stop()
	return nil
}
