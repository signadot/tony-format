package storage

import (
	"fmt"
	"sort"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// Compact compacts the inactive log according to the compaction policy.
// Removes:
// - Patches before cutoff (historical reads become approximate)
// - Superseded scope snapshots (keeps only most recent per active scope)
// - All data for deleted/inactive scopes
// - Completed/aborted schema migration entries
//
// Uses dlog.CompactInactive for file operations, then updates the index.
func (s *Storage) Compact(config *CompactionConfig) error {
	if config == nil {
		config = DefaultCompactionConfig()
	}

	if err := config.Validate(); err != nil {
		return err
	}

	s.logger.Info("starting compaction", "cutoff", config.Cutoff)

	// Get the inactive log file ID
	inactiveLogID := s.dLog.GetInactiveLog()

	// Get all segments from index for the inactive log
	allSegments := s.index.LookupRangeAll("", nil, nil)

	var inactiveSegments []index.LogSegment
	for _, seg := range allSegments {
		if dlog.LogFileID(seg.LogFile) == inactiveLogID {
			inactiveSegments = append(inactiveSegments, seg)
		}
	}

	if len(inactiveSegments) == 0 {
		s.logger.Info("no segments in inactive log, skipping compaction")
		return nil
	}

	// Find pinned commit (active schema snapshot)
	pinCommit := s.findPinnedCommit()

	// Get cutoff time
	now := time.Now()
	cutoffTime := now.Add(-config.Cutoff)

	// Select survivors
	survivors, err := s.selectSurvivors(inactiveSegments, config, now, pinCommit, cutoffTime)
	if err != nil {
		return fmt.Errorf("failed to select survivors: %w", err)
	}

	if len(survivors) == len(inactiveSegments) {
		s.logger.Info("all segments survive, skipping compaction")
		return nil
	}

	s.logger.Info("compacting",
		"original", len(inactiveSegments),
		"surviving", len(survivors))

	// Record how far back delta replay will still be exact BEFORE dropping anything, so a
	// crash in between leaves the floor too high rather than too low — pessimistic costs a
	// spurious ErrReplayCompacted, optimistic costs silent event loss. See raiseReplayFloor.
	if floor := droppedPatchFloor(inactiveSegments, survivors); floor > 0 {
		if err := s.raiseReplayFloor(floor); err != nil {
			return fmt.Errorf("failed to record replay floor: %w", err)
		}
	}

	// Extract positions to keep (sorted)
	positions := make([]int64, 0, len(survivors))
	for _, seg := range survivors {
		positions = append(positions, seg.LogPosition)
	}
	sort.Slice(positions, func(i, j int) bool { return positions[i] < positions[j] })

	// Deduplicate positions (multiple segments may reference same entry)
	positions = deduplicatePositions(positions)

	// Compact via dlog
	dlogConfig := &dlog.CompactConfig{GracePeriod: config.GracePeriod}
	results, err := s.dLog.CompactInactive(positions, dlogConfig)
	if err != nil {
		return fmt.Errorf("dlog compaction failed: %w", err)
	}

	// Build position mapping
	positionMap := make(map[int64]int64, len(results))
	for _, r := range results {
		positionMap[r.OldPosition] = r.NewPosition
	}

	// Where every entry is indexed, captured once before anything moves. Taken before
	// both maintenance passes: updateIndexPositions only touches survivors, so the
	// non-survivor entries removeFromIndex looks up are still where this says they are.
	copies := s.indexCopies()

	// Update index with new positions
	s.updateIndexPositions(inactiveLogID, survivors, positionMap, copies)

	// Remove non-surviving segments from index
	s.removeFromIndex(inactiveSegments, survivors, copies)

	s.logger.Info("compaction complete",
		"removed", len(inactiveSegments)-len(survivors))

	return nil
}

// findPinnedCommit returns the commit of the active schema snapshot, or -1 if none.
func (s *Storage) findPinnedCommit() int64 {
	_, commit := s.schema.GetActive()
	if commit > 0 {
		return commit
	}
	return -1
}

// selectSurvivors determines which segments survive compaction.
func (s *Storage) selectSurvivors(
	segments []index.LogSegment,
	config *CompactionConfig,
	now time.Time,
	pinCommit int64,
	cutoffTime time.Time,
) ([]index.LogSegment, error) {
	var survivors []index.LogSegment

	// Separate patches and snapshots
	var patches []index.LogSegment
	var snapshots []index.LogSegment

	for _, seg := range segments {
		if seg.StartCommit == seg.EndCommit {
			snapshots = append(snapshots, seg)
		} else {
			patches = append(patches, seg)
		}
	}

	// Patches: keep only those within cutoff
	for _, patch := range patches {
		// A scope's patches ARE its layer -- op-preserving, and replayed in full on
		// every scoped read, since nothing materialized can stand in for them (a scope
		// snapshot resolves !key away). So they are retained whatever the cutoff, until
		// DeleteScope removes them from the index. Bounded op-preserving compaction of
		// a scope's patch log is tracked in 5hmq80f3h12krh1mbsn0.
		if patch.ScopeID != nil {
			survivors = append(survivors, patch)
			continue
		}

		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(patch.LogFile), patch.LogPosition, patch.LogFileGeneration)
		if err != nil {
			return nil, fmt.Errorf("failed to read patch entry: %w", err)
		}

		patchTime, err := time.Parse(time.RFC3339, entry.Timestamp)
		if err != nil {
			// Can't parse timestamp, keep to be safe
			survivors = append(survivors, patch)
			continue
		}

		if patchTime.After(cutoffTime) {
			survivors = append(survivors, patch)
		}
	}

	// Snapshots: apply tier policy
	groups, err := s.buildSnapshotGroups(snapshots)
	if err != nil {
		return nil, err
	}

	policy := newCompactionPolicy(config, now, pinCommit)
	snapshotSurvivors := policy.selectSurvivors(groups)
	survivors = append(survivors, snapshotSurvivors...)

	return survivors, nil
}

// buildSnapshotGroups groups snapshots by commit and filters out:
// - aborted schema migration entries
// - superseded pending schema migration entries
func (s *Storage) buildSnapshotGroups(
	snapshots []index.LogSegment,
) ([]snapshotGroup, error) {
	// Get current pending migration state for filtering superseded pending entries
	_, pendingCommit := s.schema.GetPending()
	hasPending := s.schema.HasPending()

	// Group by commit with timestamps, filtering as we go
	byCommit := make(map[int64]*snapshotGroup)

	for _, seg := range snapshots {
		commit := seg.StartCommit

		// Lazy-init group and read entry once per commit
		group := byCommit[commit]
		if group == nil {
			entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
			if err != nil {
				return nil, fmt.Errorf("failed to read snapshot entry: %w", err)
			}

			// Check for schema migration entries to filter
			if entry.SchemaEntry != nil {
				if s.shouldSkipSchemaEntry(entry.SchemaEntry, commit, hasPending, pendingCommit) {
					continue
				}
			}

			t, err := time.Parse(time.RFC3339, entry.Timestamp)
			if err != nil {
				// If timestamp is unparseable, use current time to be safe.
				// This ensures the snapshot won't be incorrectly aged out.
				s.logger.Warn("failed to parse snapshot timestamp, using current time",
					"commit", commit, "timestamp", entry.Timestamp, "error", err)
				t = time.Now()
			}
			group = &snapshotGroup{
				commit: commit,
				time:   t,
			}
			byCommit[commit] = group
		}

		group.segments = append(group.segments, seg)
	}

	// Remove empty groups (all segments filtered out)
	for commit, group := range byCommit {
		if len(group.segments) == 0 {
			delete(byCommit, commit)
		}
	}

	// Convert to slice and sort
	groups := make([]snapshotGroup, 0, len(byCommit))
	for _, group := range byCommit {
		groups = append(groups, *group)
	}
	sortSnapshotGroups(groups)

	return groups, nil
}

// shouldSkipSchemaEntry returns true if a schema entry should be filtered out during compaction.
func (s *Storage) shouldSkipSchemaEntry(schemaEntry *dlog.SchemaEntry, commit int64, hasPending bool, pendingCommit int64) bool {
	switch schemaEntry.Status {
	case dlog.SchemaStatusAborted:
		// Aborted migrations are always safe to remove
		return true
	case dlog.SchemaStatusPending:
		// Remove superseded pending entries:
		// - No pending migration in progress (completed or aborted)
		// - Different commit than current pending (stale)
		if !hasPending || commit != pendingCommit {
			return true
		}
	}
	// Active schema entries are handled by tier policy with pinned commit
	return false
}

// entryID identifies one log entry. Every copy of that entry in the index — the root copy
// and one per path inside its patch — shares these and names the same log position, since
// indexPatchRec passes the same entry, file and position down every level of the recursion.
type entryID struct {
	startCommit int64
	startTx     int64
	scopeID     string
}

func makeEntryID(seg index.LogSegment) entryID {
	scopeID := ""
	if seg.ScopeID != nil {
		scopeID = *seg.ScopeID
	}
	return entryID{startCommit: seg.StartCommit, startTx: seg.StartTx, scopeID: scopeID}
}

// indexCopies maps each entry to every place it is indexed, keyed by entry identity.
//
// Compaction's work list comes from the root, which is the authoritative entry set — every
// entry has a root copy — but maintaining only that left every below-root copy of a moved
// or dropped entry pointing at a position that no longer holds it, which is what watch
// replay reads through (issue 1d52zghth12ks0cvcsn0).
func (s *Storage) indexCopies() map[entryID][]index.LogSegment {
	all := s.index.AllSegments()
	res := make(map[entryID][]index.LogSegment, len(all))
	for _, seg := range all {
		id := makeEntryID(seg)
		res[id] = append(res[id], seg)
	}
	return res
}

// sameEntry reports whether a copy found by identity really is the entry in hand. Identity
// should be unique, so this only guards against a copy that has already been repositioned
// or belongs to a different log file.
func sameEntry(copy, seg index.LogSegment) bool {
	return copy.LogFile == seg.LogFile && copy.LogPosition == seg.LogPosition
}

// updateIndexPositions updates segment positions in the index after compaction.
// Removes old segments and re-adds them with new positions and updated generation.
// Every copy of a moved entry is repositioned, not just the root one.
func (s *Storage) updateIndexPositions(logFileID dlog.LogFileID, survivors []index.LogSegment,
	positionMap map[int64]int64, copies map[entryID][]index.LogSegment) {
	// Get the new generation after compaction
	newGeneration := s.dLog.GetGeneration(logFileID)

	for _, seg := range survivors {
		newPos, ok := positionMap[seg.LogPosition]
		if !ok {
			continue // Position didn't change
		}

		for _, c := range copies[makeEntryID(seg)] {
			if !sameEntry(c, seg) {
				continue
			}
			// Remove the copy at its old position, re-add it at the new one. Remove and
			// Add both navigate by KindedPath, and c carries the full path it is
			// indexed at, so this reaches below-root copies as well as the root.
			s.index.Remove(&c)
			c.LogPosition = newPos
			c.LogFileGeneration = newGeneration
			s.index.Add(&c)
		}
	}
}

// segmentKey uniquely identifies a segment by its ordering key.
type segmentKey struct {
	startCommit int64
	startTx     int64
	kindedPath  string
	scopeID     string // empty string for nil
}

func makeSegmentKey(seg index.LogSegment) segmentKey {
	scopeID := ""
	if seg.ScopeID != nil {
		scopeID = *seg.ScopeID
	}
	return segmentKey{
		startCommit: seg.StartCommit,
		startTx:     seg.StartTx,
		kindedPath:  seg.KindedPath,
		scopeID:     scopeID,
	}
}

// removeFromIndex removes non-surviving segments from the index, including every
// below-root copy of each one. A non-survivor's position is freed by the rewrite, so a
// copy left behind is a segment pointing into space that now holds something else.
func (s *Storage) removeFromIndex(all, survivors []index.LogSegment, copies map[entryID][]index.LogSegment) {
	// Build set of survivor keys
	survivorSet := make(map[segmentKey]struct{}, len(survivors))
	for _, seg := range survivors {
		survivorSet[makeSegmentKey(seg)] = struct{}{}
	}

	// Remove non-survivors
	for _, seg := range all {
		if _, ok := survivorSet[makeSegmentKey(seg)]; ok {
			continue
		}
		for _, c := range copies[makeEntryID(seg)] {
			if !sameEntry(c, seg) {
				continue
			}
			s.index.Remove(&c)
		}
	}
}

// deduplicatePositions removes duplicate positions from a sorted slice.
func deduplicatePositions(positions []int64) []int64 {
	if len(positions) == 0 {
		return positions
	}

	result := make([]int64, 0, len(positions))
	result = append(result, positions[0])

	for i := 1; i < len(positions); i++ {
		if positions[i] != positions[i-1] {
			result = append(result, positions[i])
		}
	}

	return result
}
