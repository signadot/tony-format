package storage

import (
	"fmt"
	"sort"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/patches"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/snap"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// Schema operation errors
var (
	ErrMigrationInProgress   = fmt.Errorf("migration already in progress")
	ErrNoMigrationInProgress = fmt.Errorf("no migration in progress")
)

// replaySchemaState reconstructs schema state from snapshot entries during init.
// Since schema changes are coupled with snapshots, we scan snapshots in the index
// rather than iterating through the entire dlog.
// If replay ends with a pending migration, it rebuilds the pendingIndex.
func (s *Storage) replaySchemaState() error {
	// Get all segments from the index and filter for baseline snapshots
	// Use LookupRangeAll to get all segments, including multiple at same commit
	allSegments := s.index.LookupRangeAll("", nil, nil)

	// Filter for baseline snapshot segments (scopeID == nil, StartCommit == EndCommit)
	var snapshots []index.LogSegment
	for _, seg := range allSegments {
		if seg.StartCommit == seg.EndCommit && seg.ScopeID == nil {
			snapshots = append(snapshots, seg)
		}
	}

	if len(snapshots) == 0 {
		return nil // No snapshots, no schema state
	}

	// Sort by log position to ensure chronological order
	// (multiple snapshots at same commit should be in write order)
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].LogFile != snapshots[j].LogFile {
			return snapshots[i].LogFile < snapshots[j].LogFile
		}
		return snapshots[i].LogPosition < snapshots[j].LogPosition
	})

	// Track state as we replay
	var activeSchema *ir.Node
	var activeSchemaCommit int64
	var pendingSchema *ir.Node
	var pendingSchemaCommit int64

	// Process snapshots in commit order (they're already sorted)
	for _, seg := range snapshots {
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition)
		if err != nil {
			return fmt.Errorf("failed to read snapshot entry at commit %d: %w", seg.StartCommit, err)
		}

		// Skip snapshots without schema changes
		if entry.SchemaEntry == nil {
			continue
		}

		se := entry.SchemaEntry
		switch se.Status {
		case dlog.SchemaStatusPending:
			pendingSchema = se.Schema
			pendingSchemaCommit = entry.Commit

		case dlog.SchemaStatusActive:
			// If we had a pending migration, it's now complete
			activeSchema = se.Schema
			activeSchemaCommit = entry.Commit
			pendingSchema = nil
			pendingSchemaCommit = 0

		case dlog.SchemaStatusAborted:
			// Migration was aborted, clear pending state
			pendingSchema = nil
			pendingSchemaCommit = 0
		}
	}

	// Apply final state (no lock needed during init - single goroutine)
	s.schema.SetActive(activeSchema, activeSchemaCommit)

	// If we ended with a pending migration, rebuild the pending index
	if pendingSchema != nil {
		pendingIdx := index.NewIndex("")
		pendingParsed := api.ParseSchemaFromNode(pendingSchema)
		s.schema.SetPending(pendingSchema, pendingSchemaCommit, pendingIdx, pendingParsed)

		// Get current commit - we need to re-index everything from activeSchemaCommit
		// to current, not just to pendingSchemaCommit. Data written during the migration
		// (after pendingSchemaCommit) was dual-indexed originally and must be included.
		currentCommit := s.getIndexMaxCommit()
		if currentCommit < 0 {
			currentCommit = pendingSchemaCommit
		}

		if err := s.reindexForPending(activeSchemaCommit, currentCommit); err != nil {
			s.schema.ClearPending()
			return fmt.Errorf("failed to rebuild pending index: %w", err)
		}

		s.logger.Info("restored pending migration state",
			"activeSchemaCommit", activeSchemaCommit,
			"pendingSchemaCommit", pendingSchemaCommit,
			"reindexedTo", currentCommit)
	} else if activeSchema != nil {
		s.logger.Info("restored schema state", "activeSchemaCommit", activeSchemaCommit)
	}

	return nil
}

// StartMigration begins a schema migration by setting a pending schema.
// Returns ErrMigrationInProgress if a migration is already in progress.
// This creates a snapshot with the pending schema and starts building a new index.
func (s *Storage) StartMigration(schema *ir.Node) (int64, error) {
	if s.schema.HasPending() {
		return 0, ErrMigrationInProgress
	}

	commit, err := s.createSchemaSnapshot(schema, dlog.SchemaStatusPending, nil)
	if err != nil {
		return 0, err
	}

	pendingIdx := index.NewIndex("")
	pendingParsed := api.ParseSchemaFromNode(schema)
	s.schema.SetPending(schema, commit, pendingIdx, pendingParsed)

	// Re-index existing data from activeSchemaCommit to commit
	_, activeSchemaCommit := s.schema.GetActive()
	if err := s.reindexForPending(activeSchemaCommit, commit); err != nil {
		// Clear pending state on failure
		s.schema.ClearPending()
		return 0, fmt.Errorf("failed to re-index for pending schema: %w", err)
	}

	return commit, nil
}

// CompleteMigration completes a pending schema migration.
// Returns ErrNoMigrationInProgress if no migration is in progress.
// This creates a snapshot with the active schema and swaps the indexes.
func (s *Storage) CompleteMigration() (int64, error) {
	pendingSchema, _ := s.schema.GetPending()
	if pendingSchema == nil {
		return 0, ErrNoMigrationInProgress
	}

	commit, err := s.createSchemaSnapshot(pendingSchema, dlog.SchemaStatusActive, nil)
	if err != nil {
		return 0, err
	}

	// Promote pending to active and get new index
	newIndex := s.schema.PromotePending(commit)
	s.index = newIndex

	return commit, nil
}

// AbortMigration aborts a pending schema migration.
// Returns ErrNoMigrationInProgress if no migration is in progress.
func (s *Storage) AbortMigration() (int64, error) {
	if !s.schema.HasPending() {
		return 0, ErrNoMigrationInProgress
	}

	commit, err := s.createSchemaSnapshot(nil, dlog.SchemaStatusAborted, nil)
	if err != nil {
		return 0, err
	}

	s.schema.ClearPending()

	return commit, nil
}

// MigrationPatch applies a patch during migration.
// The patch is written to baseline (scopeID=nil) but only indexed into pendingIndex.
// It becomes visible in baseline when migration completes.
// Returns ErrNoMigrationInProgress if no migration is in progress.
func (s *Storage) MigrationPatch(path string, patch *ir.Node) (int64, *ir.Node, error) {
	pendingSchema, _ := s.schema.GetPending()
	if pendingSchema == nil {
		return 0, nil, ErrNoMigrationInProgress
	}
	pendingIdx := s.schema.GetPendingIndex()

	// Get next commit number
	commit, err := s.sequence.NextCommit()
	if err != nil {
		return 0, nil, fmt.Errorf("failed to get next commit: %w", err)
	}

	// Get current commit for lastCommit
	currentCommit, err := s.GetCurrentCommit()
	if err != nil {
		return 0, nil, fmt.Errorf("failed to get current commit: %w", err)
	}
	lastCommit := currentCommit - 1
	if lastCommit < 0 {
		lastCommit = 0
	}

	// Add PatchRootTag so StreamingProcessor can identify the patch root
	patch.Tag = ir.TagCompose(tx.PatchRootTag, nil, patch.Tag)

	// Build the root patch with path
	rootPatch := buildRootPatch(path, patch)

	// Create entry as baseline (scopeID = nil)
	timestamp := time.Now().UTC().Format(time.RFC3339)
	entry := dlog.NewEntry(nil, rootPatch, commit, timestamp, lastCommit, nil)

	// Write to dlog
	pos, logFile, err := s.dLog.AppendEntry(entry)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to append entry: %w", err)
	}

	// Parse the pending schema for indexing
	parsedSchema := api.ParseSchemaFromNode(pendingSchema)

	// Index ONLY into pendingIndex (not the active index)
	if err := index.IndexPatch(pendingIdx, entry, string(logFile), pos, 0, rootPatch, parsedSchema, nil); err != nil {
		return 0, nil, fmt.Errorf("failed to index patch: %w", err)
	}

	s.logger.Info("migration patch applied", "commit", commit, "path", path)
	return commit, rootPatch, nil
}

// buildRootPatch wraps a patch at a path into a root patch.
func buildRootPatch(path string, patch *ir.Node) *ir.Node {
	if path == "" {
		return patch
	}

	// Parse path and build nested structure
	// For simplicity, handle dot-separated paths
	parts := splitPath(path)
	result := patch
	for i := len(parts) - 1; i >= 0; i-- {
		result = ir.FromMap(map[string]*ir.Node{
			parts[i]: result,
		})
	}
	return result
}

// splitPath splits a kpath into parts (simplified, handles dots only)
func splitPath(path string) []string {
	if path == "" {
		return nil
	}
	var parts []string
	current := ""
	for _, c := range path {
		if c == '.' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

// reindexForPending re-indexes existing data into the pending index.
// It uses the existing index to find segments in the commit range, then reads
// only those entries from the dlog (avoiding a full scan).
func (s *Storage) reindexForPending(fromCommit, toCommit int64) error {
	pendingIdx := s.schema.GetPendingIndex()
	if pendingIdx == nil {
		return fmt.Errorf("no pending migration in progress")
	}

	// Use cached parsed schema
	parsedSchema := s.schema.GetPendingParsed()

	// Use the index to find segments in the commit range (all scopes)
	// This avoids scanning the entire dlog from the beginning
	segments := s.index.LookupRangeAll("", &fromCommit, &toCommit)

	indexedCount := 0
	for _, seg := range segments {
		// Skip snapshots (StartCommit == EndCommit) - they don't have patches
		if seg.StartCommit == seg.EndCommit {
			continue
		}

		// Skip entries at or before fromCommit (LookupRangeAll is inclusive)
		if seg.EndCommit <= fromCommit {
			continue
		}

		// Read entry from dlog
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition)
		if err != nil {
			return fmt.Errorf("failed to read entry at commit %d: %w", seg.EndCommit, err)
		}

		// Skip entries without patches
		if entry.Patch == nil {
			continue
		}

		// Index into pending index
		if err := index.IndexPatch(pendingIdx, entry, seg.LogFile, seg.LogPosition, seg.EndTx, entry.Patch, parsedSchema, entry.ScopeID); err != nil {
			return fmt.Errorf("failed to index entry at commit %d: %w", entry.Commit, err)
		}
		indexedCount++
	}

	s.logger.Info("re-indexed for pending schema", "fromCommit", fromCommit, "toCommit", toCommit, "entries", indexedCount)
	return nil
}

// createSchemaSnapshot creates a snapshot with a schema change entry.
// This is similar to createSnapshot but includes the SchemaEntry.
// Each schema snapshot gets its own commit number to avoid duplicates at the same commit.
func (s *Storage) createSchemaSnapshot(schema *ir.Node, status string, scopeID *string) (int64, error) {
	// Allocate a new commit number for this schema snapshot
	commit, err := s.sequence.NextCommit()
	if err != nil {
		return 0, fmt.Errorf("failed to get next commit: %w", err)
	}

	// Find most recent snapshot and get base event reader (up to previous commit)
	prevCommit := commit - 1
	if prevCommit < 0 {
		prevCommit = 0
	}
	baseReader, startCommit, err := s.findSnapshotBaseReader("", prevCommit, scopeID)
	if err != nil {
		return 0, err
	}
	defer baseReader.Close()

	// Get patches from startCommit to prevCommit
	segments := s.index.LookupRange("", &startCommit, &prevCommit, scopeID)

	// Extract patch nodes, filtering out snapshots
	var patchNodes []*ir.Node
	for _, seg := range segments {
		// Skip snapshots (StartCommit == EndCommit)
		if seg.StartCommit == seg.EndCommit {
			continue
		}

		// Read patch from dlog
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition)
		if err != nil {
			return 0, fmt.Errorf("failed to read patch entry: %w", err)
		}
		if entry.Patch == nil {
			continue
		}

		patchNodes = append(patchNodes, entry.Patch)
	}

	// Create snapshot writer for inactive log
	timestamp := time.Now().UTC().Format(time.RFC3339)
	snapWriter, err := s.dLog.NewSnapshotWriter(commit, timestamp)
	if err != nil {
		return 0, fmt.Errorf("failed to create snapshot writer: %w", err)
	}
	snapWriter.SetScopeID(scopeID)
	snapWriter.SetSchemaEntry(&dlog.SchemaEntry{
		Schema: schema,
		Status: status,
	})

	// Build snapshot directly to log file (out-of-memory)
	snapIndex := &snap.Index{}
	builder, err := snap.NewBuilder(snapWriter, snapIndex, patchNodes)
	if err != nil {
		snapWriter.Abandon() // Unlock without writing Entry
		return 0, fmt.Errorf("failed to create snapshot builder: %w", err)
	}

	// Apply patches - events flow directly from baseReader → builder → log file
	applier := patches.NewStreamingProcessor()
	if err := applier.ApplyPatches(baseReader, patchNodes, builder); err != nil {
		snapWriter.Abandon()
		return 0, fmt.Errorf("failed to apply patches: %w", err)
	}

	// Close builder to finalize snapshot format (writes index and header)
	// Note: builder.Close() will call snapWriter.Close(), which writes the Entry
	if err := builder.Close(); err != nil {
		return 0, fmt.Errorf("failed to close snapshot builder: %w", err)
	}

	snapSeg := &index.LogSegment{
		StartCommit: commit,
		EndCommit:   commit,
		StartTx:     0,
		EndTx:       0,
		KindedPath:  "",
		LogFile:     string(snapWriter.LogFileID()),
		LogPosition: snapWriter.EntryPosition(),
		ScopeID:     scopeID,
	}
	s.index.Add(snapSeg)

	s.logger.Info("schema snapshot created", "commit", commit, "status", status, "logFile", snapWriter.LogFileID(), "position", snapWriter.EntryPosition())
	return commit, nil
}
