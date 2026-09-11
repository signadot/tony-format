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
//   - Baseline patches before cutoff (historical reads become approximate)
//   - Snapshots of paths before cutoff (path_snapshot.go)
//   - A scope's entries before cutoff that a later entry of the scope dominates
//     (scope_compaction.go)
//   - Root snapshots the tiers do not keep
//   - Completed/aborted schema migration entries
//
// THE WORK LIST IS THE LOG'S OWN RECORDS. Compaction walks the inactive file once and
// decides each entry's fate from the entry -- its time, its scope, whether it is a
// snapshot and of what -- and never asks the index for every segment it holds: what the
// index describes is every path of every entry in both logs, and a compaction that holds
// that holds the store (index_residency.md). What it holds is one record per entry in the
// file being compacted, the survivors' positions, which dlog needs, and one entry at a
// time while that entry's segments are removed or moved (index.EachSegment). A snapshot
// of a path is indexed only at its path, so a work list taken from the root's segments
// would not see it; the file does.
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

	inactiveLogID := s.dLog.GetInactiveLog()
	records, err := s.compactionRecords(inactiveLogID)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		s.logger.Info("no entries in inactive log, skipping compaction")
		return nil
	}

	// Get cutoff time
	now := time.Now()
	cutoffTime := now.Add(-config.Cutoff)

	// A scope's entries beyond the cutoff go when a later entry of the scope dominates
	// them (scope_compaction.go). Deciding that reads the scope, so it is asked once, for
	// the scopes the file holds, and answered by position.
	candidates := map[int64]*string{}
	for _, r := range records {
		if r.seg.ScopeID != nil && r.seg.StartCommit != r.seg.EndCommit && r.timeOK && !r.time.After(cutoffTime) {
			candidates[r.pos] = r.seg.ScopeID
		}
	}
	dominated, err := s.dominatedScopeEntries(inactiveLogID, candidates)
	if err != nil {
		return err
	}

	survivors, dropped := s.selectSurvivors(records, config, now, cutoffTime, dominated)
	if len(dropped) == 0 {
		s.logger.Info("all entries survive, skipping compaction")
		return nil
	}

	s.logger.Info("compacting",
		"original", len(records),
		"surviving", len(survivors))

	// Record how far back delta replay will still be exact BEFORE dropping anything, so a
	// crash in between leaves the floor too high rather than too low — pessimistic costs a
	// spurious ErrReplayCompacted, optimistic costs silent event loss. See raiseReplayFloor.
	if floor := droppedPatchFloor(segmentsOf(records), segmentsOf(survivors)); floor > 0 {
		if err := s.raiseReplayFloor(floor); err != nil {
			return fmt.Errorf("failed to record replay floor: %w", err)
		}
	}

	// A dropped entry leaves the index before the file is rewritten, while it can still be
	// read from where it is: its segments are derived from it and each is removed. A reader
	// in between sees the index compaction will leave, over a file that still holds more,
	// which is consistent; and the index is rebuilt from the log on a crash.
	oldGeneration := s.dLog.GetGeneration(inactiveLogID)
	for _, r := range dropped {
		if err := s.unindexEntry(inactiveLogID, r.pos, oldGeneration); err != nil {
			return err
		}
	}

	positions := make([]int64, 0, len(survivors))
	for _, r := range survivors {
		positions = append(positions, r.pos)
	}
	sort.Slice(positions, func(i, j int) bool { return positions[i] < positions[j] })
	positions = deduplicatePositions(positions)

	dlogConfig := &dlog.CompactConfig{GracePeriod: config.GracePeriod}
	results, err := s.dLog.CompactInactive(positions, dlogConfig)
	if err != nil {
		return fmt.Errorf("dlog compaction failed: %w", err)
	}
	positionMap := make(map[int64]int64, len(results))
	for _, r := range results {
		positionMap[r.OldPosition] = r.NewPosition
	}

	// A survivor is re-indexed where the rewrite put it, read back from there: every copy
	// of it, at every path, under the file's new generation.
	newGeneration := s.dLog.GetGeneration(inactiveLogID)
	for _, r := range survivors {
		newPos, ok := positionMap[r.pos]
		if !ok {
			continue
		}
		if err := s.reindexEntry(inactiveLogID, newPos, newGeneration); err != nil {
			return err
		}
	}

	s.logger.Info("compaction complete", "removed", len(dropped))
	return nil
}

// compactRecord is what compaction holds per entry of the file it compacts: the entry's
// place and shape, said as the root-level segment the entry would be indexed by -- which
// is what the policy and the replay floor read -- and its time, so no entry is read twice
// to decide its fate. keep marks an entry compaction never drops: a schema commit, which
// is the record of when the schema changed and is small (schema.go).
type compactRecord struct {
	pos    int64
	seg    index.LogSegment
	time   time.Time
	timeOK bool
	keep   bool
}

// compactionRecords walks one log file and answers a record per entry.
func (s *Storage) compactionRecords(logFile dlog.LogFileID) ([]compactRecord, error) {
	it, err := s.dLog.FileIterator(logFile)
	if err != nil {
		return nil, err
	}
	generation := s.dLog.GetGeneration(logFile)
	var records []compactRecord
	for {
		entry, pos, err := it.Next()
		if err != nil {
			break // io.EOF, or a record the walk could not read: what is behind it is not compacted this time
		}
		r := compactRecord{pos: pos}
		switch {
		case entry.SnapPos != nil:
			r.seg = *index.NewSnapshotSegment(entry.Commit, index.SnapPathOf(entry), string(logFile), pos, generation, entry.ScopeID)
		case entry.Patch != nil && entry.LastCommit != nil:
			r.seg = *index.NewLogSegmentFromPatchEntry(entry, "", string(logFile), pos, index.TxSeqOf(entry), generation, entry.ScopeID)
		case entry.IsSchemaCommit():
			// Not indexed -- it lands nowhere -- and kept: an entry the records did not
			// name was left out of the rewrite, which is how a schema commit would have
			// gone silently.
			r.seg = index.LogSegment{StartCommit: *entry.LastCommit, EndCommit: entry.Commit, LogFile: string(logFile), LogPosition: pos, LogFileGeneration: generation}
			r.keep = true
		default:
			continue // not something the index describes
		}
		if t, err := time.Parse(time.RFC3339, entry.Timestamp); err == nil {
			r.time, r.timeOK = t, true
		}
		records = append(records, r)
	}
	return records, nil
}

func segmentsOf(records []compactRecord) []index.LogSegment {
	out := make([]index.LogSegment, len(records))
	for i, r := range records {
		out[i] = r.seg
	}
	return out
}

// selectSurvivors decides which entries survive compaction, and which go.
func (s *Storage) selectSurvivors(
	records []compactRecord,
	config *CompactionConfig,
	now time.Time,
	cutoffTime time.Time,
	dominated map[int64]bool,
) (survivors, dropped []compactRecord) {
	// Root snapshots are what the tiers keep; everything else -- the writes, and the
	// snapshots of paths that stand in for a run of them (path_snapshot.go) -- is kept
	// within the cutoff and dropped beyond it. A snapshot of a path is an accelerator
	// for reads at the head and not a point history is read at, so it takes no tier
	// slot from the root snapshots that are.
	var rootSnapshots []compactRecord
	for _, r := range records {
		if r.keep {
			survivors = append(survivors, r)
			continue
		}
		if r.seg.StartCommit == r.seg.EndCommit && r.seg.KindedPath == "" {
			rootSnapshots = append(rootSnapshots, r)
			continue
		}
		// A scope's entries ARE its layer, replayed on every scoped read, and nothing
		// materialized stands in for them. One goes only when a later entry of the
		// scope dominates it -- states everything it stated, in a way that does not
		// depend on what was there -- and it is beyond the cutoff (scope_compaction.go).
		if r.seg.ScopeID != nil {
			if dominated[r.pos] {
				dropped = append(dropped, r)
			} else {
				survivors = append(survivors, r)
			}
			continue
		}
		// A time it cannot read keeps an entry, to be safe.
		if !r.timeOK || r.time.After(cutoffTime) {
			survivors = append(survivors, r)
		} else {
			dropped = append(dropped, r)
		}
	}

	// Root snapshots: apply tier policy, on the groups the records make.
	groups := s.buildSnapshotGroups(rootSnapshots)
	policy := newCompactionPolicy(config, now)
	kept := make(map[int64]bool)
	for _, seg := range policy.selectSurvivors(groups) {
		kept[seg.LogPosition] = true
	}
	for _, r := range rootSnapshots {
		if kept[r.pos] {
			survivors = append(survivors, r)
		} else {
			dropped = append(dropped, r)
		}
	}
	return survivors, dropped
}

// buildSnapshotGroups groups root snapshots by commit. Every root snapshot records the
// schema in force at its commit, so none needs keeping for the schema's sake: whichever
// survive carry it.
func (s *Storage) buildSnapshotGroups(snapshots []compactRecord) []snapshotGroup {
	byCommit := make(map[int64]*snapshotGroup)
	for _, r := range snapshots {
		commit := r.seg.StartCommit
		group := byCommit[commit]
		if group == nil {
			t := r.time
			if !r.timeOK {
				// If timestamp is unparseable, use current time to be safe.
				// This ensures the snapshot won't be incorrectly aged out.
				s.logger.Warn("failed to parse snapshot timestamp, using current time", "commit", commit)
				t = time.Now()
			}
			group = &snapshotGroup{commit: commit, time: t}
			byCommit[commit] = group
		}
		group.segments = append(group.segments, r.seg)
	}

	groups := make([]snapshotGroup, 0, len(byCommit))
	for _, group := range byCommit {
		groups = append(groups, *group)
	}
	sortSnapshotGroups(groups)
	return groups
}

// unindexEntry removes every segment of the entry at pos from the index, deriving them
// from the entry itself.
func (s *Storage) unindexEntry(logFile dlog.LogFileID, pos, generation int64) error {
	entry, err := s.dLog.ReadEntryAt(logFile, pos, generation)
	if err != nil {
		return fmt.Errorf("compaction: read entry at %s@%d: %w", logFile, pos, err)
	}
	index.EachSegment(entry, string(logFile), pos, generation, func(seg *index.LogSegment) {
		s.index.Remove(seg)
	})
	return nil
}

// reindexEntry moves every segment of the entry now at pos to where it is: each is removed
// by its key, which does not depend on where the entry was, and added with the position
// and generation it has now.
func (s *Storage) reindexEntry(logFile dlog.LogFileID, pos, generation int64) error {
	entry, err := s.dLog.ReadEntryAt(logFile, pos, generation)
	if err != nil {
		return fmt.Errorf("compaction: read entry at %s@%d: %w", logFile, pos, err)
	}
	index.EachSegment(entry, string(logFile), pos, generation, func(seg *index.LogSegment) {
		s.index.Remove(seg)
		s.index.Add(seg)
	})
	return nil
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
