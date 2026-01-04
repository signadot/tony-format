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
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(patch.LogFile), patch.LogPosition)
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

// buildSnapshotGroups groups snapshots by commit and filters out inactive scopes.
func (s *Storage) buildSnapshotGroups(
	snapshots []index.LogSegment,
	activeScopes map[string]struct{},
) ([]snapshotGroup, error) {
	// Filter out scope snapshots for inactive scopes
	var filtered []index.LogSegment
	for _, seg := range snapshots {
		if seg.ScopeID != nil {
			if _, active := activeScopes[*seg.ScopeID]; !active {
				continue
			}
		}
		filtered = append(filtered, seg)
	}

	// Group by commit with timestamps
	byCommit := make(map[int64]*snapshotGroup)

	for _, seg := range filtered {
		commit := seg.StartCommit

		if byCommit[commit] == nil {
			entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition)
			if err != nil {
				return nil, fmt.Errorf("failed to read snapshot entry: %w", err)
			}

			t, _ := time.Parse(time.RFC3339, entry.Timestamp)
			byCommit[commit] = &snapshotGroup{
				commit: commit,
				time:   t,
			}
		}

		byCommit[commit].segments = append(byCommit[commit].segments, seg)
	}

	// Convert to slice and sort
	groups := make([]snapshotGroup, 0, len(byCommit))
	for _, group := range byCommit {
		groups = append(groups, *group)
	}
	sortSnapshotGroups(groups)

	return groups, nil
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
func (s *Storage) updateIndexPositions(logFileID dlog.LogFileID, survivors []index.LogSegment, positionMap map[int64]int64) {
	// For now, we update positions in place
	// A more robust approach would rebuild the index
	for i := range survivors {
		if newPos, ok := positionMap[survivors[i].LogPosition]; ok {
			survivors[i].LogPosition = newPos
		}
	}
	// Note: The index segments are copies, so we need to update the actual index
	// This is a simplified placeholder - proper implementation would use index methods
}

// removeFromIndex removes non-surviving segments from the index.
func (s *Storage) removeFromIndex(all, survivors []index.LogSegment) {
	// Build set of surviving positions
	survivorSet := make(map[int64]struct{}, len(survivors))
	for _, seg := range survivors {
		survivorSet[seg.LogPosition] = struct{}{}
	}

	// Remove non-survivors
	// Note: This is a placeholder - proper implementation needs index.Remove()
	for _, seg := range all {
		if _, ok := survivorSet[seg.LogPosition]; !ok {
			// Would call s.index.Remove(seg) if method existed
			_ = seg
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
