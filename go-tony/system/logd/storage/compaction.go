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

	// Update index with new positions
	s.updateIndexPositions(inactiveLogID, survivors, positionMap)

	// Remove non-surviving segments from index
	s.removeFromIndex(inactiveSegments, survivors)

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

	// Get active scopes
	activeScopes := s.getActiveScopes()

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
	groups, err := s.buildSnapshotGroups(snapshots, activeScopes)
	if err != nil {
		return nil, err
	}

	policy := newCompactionPolicy(config, now, pinCommit)
	snapshotSurvivors := policy.selectSurvivors(groups)
	survivors = append(survivors, snapshotSurvivors...)

	return survivors, nil
}

// buildSnapshotGroups groups snapshots by commit and filters out:
// - scope snapshots for inactive scopes
// - aborted schema migration entries
// - superseded pending schema migration entries
func (s *Storage) buildSnapshotGroups(
	snapshots []index.LogSegment,
	activeScopes map[string]struct{},
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

			t, _ := time.Parse(time.RFC3339, entry.Timestamp)
			group = &snapshotGroup{
				commit: commit,
				time:   t,
			}
			byCommit[commit] = group
		}

		// Filter out scope snapshots for inactive scopes
		if seg.ScopeID != nil {
			if _, active := activeScopes[*seg.ScopeID]; !active {
				continue
			}
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

// getActiveScopes returns the set of currently active scope IDs.
func (s *Storage) getActiveScopes() map[string]struct{} {
	s.activeScopesMu.RLock()
	defer s.activeScopesMu.RUnlock()

	result := make(map[string]struct{}, len(s.activeScopes))
	for scopeID := range s.activeScopes {
		result[scopeID] = struct{}{}
	}
	return result
}

// updateIndexPositions updates segment positions in the index after compaction.
// Removes old segments and re-adds them with new positions.
func (s *Storage) updateIndexPositions(logFileID dlog.LogFileID, survivors []index.LogSegment, positionMap map[int64]int64) {
	for _, seg := range survivors {
		newPos, ok := positionMap[seg.LogPosition]
		if !ok {
			continue // Position didn't change
		}

		// Remove segment with old position
		s.index.Remove(&seg)

		// Add segment with new position
		seg.LogPosition = newPos
		s.index.Add(&seg)
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

// removeFromIndex removes non-surviving segments from the index.
func (s *Storage) removeFromIndex(all, survivors []index.LogSegment) {
	// Build set of survivor keys
	survivorSet := make(map[segmentKey]struct{}, len(survivors))
	for _, seg := range survivors {
		survivorSet[makeSegmentKey(seg)] = struct{}{}
	}

	// Remove non-survivors
	for _, seg := range all {
		if _, ok := survivorSet[makeSegmentKey(seg)]; !ok {
			s.index.Remove(&seg)
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
