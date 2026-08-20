package storage

import (
	"fmt"
	"slices"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// ReadPatchesInRange reads all patches affecting the given kpath in the commit range [from, to].
// Returns CommitNotifications sorted by commit number, deduplicated.
// This is used for watch replay - returning patches that affect the watched path.
// scopeID controls filtering: nil = baseline only, non-nil = baseline + scope.
//
// Returns ErrReplayCompacted if the range starts at or below the replay floor, rather
// than the subset that happens to survive. The caller asked for every change in the
// range and cannot be given it, and an empty or short result is indistinguishable from a
// quiet period — so this is reported, not returned as data. A scoped range is checked
// too: a scope keeps its own overlay in full, but the baseline patches its replay
// interleaves with are subject to the same cutoff.
func (s *Storage) ReadPatchesInRange(kp string, from, to int64, scopeID *string) ([]*CommitNotification, error) {
	if floor := s.replayFloor.Load(); from <= floor {
		return nil, fmt.Errorf("%w: range starts at %d, exact from %d", ErrReplayCompacted, from, floor+1)
	}

	segments := s.index.LookupRange(kp, &from, &to, scopeID)
	if len(segments) == 0 {
		return nil, nil
	}

	// Deduplicate by commit - same patch entry may be indexed at multiple paths
	seen := make(map[int64]bool)
	var result []*CommitNotification

	for _, seg := range segments {
		// Skip if we've already processed this commit
		if seen[seg.EndCommit] {
			continue
		}
		seen[seg.EndCommit] = true

		// Read entry from dlog
		logFile := dlog.LogFileID(seg.LogFile)
		entry, err := s.dLog.ReadEntryAt(logFile, seg.LogPosition, seg.LogFileGeneration)
		if err != nil {
			return nil, fmt.Errorf("failed to read entry at %s:%d: %w", seg.LogFile, seg.LogPosition, err)
		}

		if entry.Patch == nil {
			continue
		}

		// Build CommitNotification. The patch goes out in the form a client sees --
		// its own copy, with the internal markers gone -- which is what the live path
		// publishes and what this used to skip. See DeliverablePatch.
		notification := &CommitNotification{
			Commit:    entry.Commit,
			Timestamp: entry.Timestamp,
			Patch:     DeliverablePatch(entry.Patch),
			// KPaths not populated - would need index lookup per entry
		}

		result = append(result, notification)
	}

	// Sort by commit (segments are already sorted, but dedup may have changed order)
	slices.SortFunc(result, func(a, b *CommitNotification) int {
		if a.Commit < b.Commit {
			return -1
		}
		if a.Commit > b.Commit {
			return 1
		}
		return 0
	})

	return result, nil
}
