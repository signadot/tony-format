package storage

import (
	"fmt"
	"slices"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// EachPatchInRange calls fn for every patch affecting kp in the commit range [from, to],
// in commit order, deduplicated. scopeID controls filtering: nil = baseline only,
// non-nil = baseline + scope.
//
// It reads ONE entry at a time. The range is resolved to segments up front, which is
// cheap -- a segment is a few words and the index already holds them -- and the dedup
// and the ordering are settled there, before anything is decoded. That is the whole
// point of the shape: a replay's peak is one entry rather than the range.
//
// Collecting the range first cost about 65KB per commit, so a 20k-commit catch-up moved
// 1.3GB through the heap; a client asking for a whole 225k-commit history drove the
// process to 3.7-7.1GB and the kubelet evicted the pod, four times in fifteen minutes,
// taking the store down for everyone else (89my9f0kh12ksqknjhn0).
//
// An error from fn stops the walk and is returned as-is, so a caller can abandon the
// range without draining it.
//
// Returns ErrReplayCompacted if the range starts at or below the replay floor, rather
// than the subset that happens to survive. The caller asked for every change in the
// range and cannot be given it, and an empty or short result is indistinguishable from a
// quiet period -- so this is reported, not returned as data. A scoped range is checked
// too: a scope's own patches are kept in full, but the baseline patches its replay
// interleaves with are subject to the same cutoff.
//
// That check is made before fn is called at all, so the compacted refusal still precedes
// any delivery. What a streaming walk cannot promise is the same for a dlog read that
// fails PART WAY: earlier patches have already gone to fn by then. Every caller here
// ends the watch on the error and the terminal event carries what was accounted for, so
// the client resumes from where the walk got to rather than losing it.
func (s *Storage) EachPatchInRange(kp string, from, to int64, scopeID *string, fn func(*CommitNotification) error) error {
	if floor := s.replayFloor.Load(); from <= floor {
		return fmt.Errorf("%w: range starts at %d, exact from %d", ErrReplayCompacted, from, floor+1)
	}

	// Dedup and order in SEGMENT space, before any entry is read. A patch is indexed
	// once per path inside it, so a range read at a shallow kp meets the same commit
	// many times. LookupRange returns its own slice, already ordered by StartCommit,
	// which for a patch segment is its commit less one -- so ordering the segments is
	// ordering the commits, and the sort the collecting version did afterwards was
	// sorting an already-sorted list.
	segments := s.index.LookupRange(kp, &from, &to, scopeID)
	seen := make(map[int64]bool, len(segments))
	segments = slices.DeleteFunc(segments, func(seg index.LogSegment) bool {
		if seen[seg.EndCommit] {
			return true
		}
		seen[seg.EndCommit] = true
		return false
	})

	for i := range segments {
		seg := &segments[i]
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
		if err != nil {
			return fmt.Errorf("failed to read entry at %s:%d: %w", seg.LogFile, seg.LogPosition, err)
		}
		if entry.Patch == nil {
			continue
		}
		// The patch goes out in the form a client sees -- its own copy, with the
		// internal markers gone -- which is what the live path publishes. See
		// DeliverablePatch.
		if err := fn(&CommitNotification{
			Commit:    entry.Commit,
			Timestamp: entry.Timestamp,
			Patch:     DeliverablePatch(entry.Patch),
			// KPaths not populated - would need index lookup per entry
		}); err != nil {
			return err
		}
	}
	return nil
}

// ReadPatchesInRange collects what EachPatchInRange walks. It holds the whole range in
// memory, so it is for a caller that knows its range is small -- a test, or a read of a
// single commit. A watch replay streams instead; see EachPatchInRange.
func (s *Storage) ReadPatchesInRange(kp string, from, to int64, scopeID *string) ([]*CommitNotification, error) {
	var result []*CommitNotification
	err := s.EachPatchInRange(kp, from, to, scopeID, func(n *CommitNotification) error {
		result = append(result, n)
		return nil
	})
	return result, err
}
