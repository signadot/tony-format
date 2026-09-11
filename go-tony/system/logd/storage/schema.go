package storage

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/patches"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/snap"
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
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
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
// This creates a snapshot with the pending schema and starts building a new index,
// and answers the snapshot's commit.
//
// A schema that does not validate is refused, and so is one that changes an array's
// identity in a way the stored data cannot follow: an array losing or changing its
// identity, or gaining one while it holds elements written by position.
func (s *Storage) StartMigration(schema *ir.Node) (int64, error) {
	if s.schema.HasPending() {
		return 0, ErrMigrationInProgress
	}

	// Reject a schema that cannot mean what it says BEFORE it is written. Key derivation
	// decides what a stored delta records, and a delta cannot be un-recorded, so an
	// ambiguous schema is caught where it is proposed rather than where it bites.
	if err := api.ParseSchemaFromNode(schema).Validate(); err != nil {
		return 0, fmt.Errorf("schema cannot be adopted: %w", err)
	}
	if err := s.identityChangeAllowed(api.ParseSchemaFromNode(schema)); err != nil {
		return 0, fmt.Errorf("schema cannot be adopted: %w", err)
	}

	commit, err := s.createSchemaSnapshot(schema, dlog.SchemaStatusPending)
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

	commit, err := s.createSchemaSnapshot(pendingSchema, dlog.SchemaStatusActive)
	if err != nil {
		return 0, err
	}

	// Promote pending to active and get new index
	newIndex := s.schema.PromotePending(commit)
	newIndex.Adopt(s.index) // the live index's residency and file; the next persist writes it
	s.index = newIndex

	return commit, nil
}

// AbortMigration aborts a pending schema migration.
// Returns ErrNoMigrationInProgress if no migration is in progress.
func (s *Storage) AbortMigration() (int64, error) {
	if !s.schema.HasPending() {
		return 0, ErrNoMigrationInProgress
	}

	commit, err := s.createSchemaSnapshot(nil, dlog.SchemaStatusAborted)
	if err != nil {
		return 0, err
	}

	s.schema.ClearPending()

	return commit, nil
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
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
		if err != nil {
			return fmt.Errorf("failed to read entry at commit %d: %w", seg.EndCommit, err)
		}

		// Skip entries without patches
		if entry.Patch == nil {
			continue
		}

		// Index into pending index
		index.IndexPatch(pendingIdx, entry, seg.LogFile, seg.LogPosition, seg.EndTx, seg.LogFileGeneration, entry.Patch, entry.ScopeID)
		indexedCount++
	}

	s.logger.Info("re-indexed for pending schema", "fromCommit", fromCommit, "toCommit", toCommit, "entries", indexedCount)
	return nil
}

// createSchemaSnapshot creates a snapshot with a schema change entry.
// This is similar to createSnapshot but includes the SchemaEntry.
// Each schema snapshot gets its own commit number to avoid duplicates at the same commit.
func (s *Storage) createSchemaSnapshot(schema *ir.Node, status string) (int64, error) {
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
	baseReader, startCommit, err := s.findSnapshotBaseReader(prevCommit)
	if err != nil {
		return 0, err
	}
	defer baseReader.Close()

	patchNodes, err := s.patchesSince(startCommit, prevCommit)
	if err != nil {
		return 0, err
	}

	// Create snapshot writer for inactive log
	timestamp := time.Now().UTC().Format(time.RFC3339)
	snapWriter, err := s.dLog.NewSnapshotWriter(commit, timestamp)
	if err != nil {
		return 0, fmt.Errorf("failed to create snapshot writer: %w", err)
	}
	snapWriter.SetScopeID(nil)
	snapWriter.SetSchemaEntry(&dlog.SchemaEntry{
		Schema: schema,
		Status: status,
	})

	// Build snapshot directly to log file (out-of-memory)
	snapIndex := &snap.Index{}
	builder, err := snap.NewBuilder(snapWriter, snapIndex)
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

	// Get generation for the snapshot segment
	generation := s.dLog.GetGeneration(snapWriter.LogFileID())

	snapSeg := &index.LogSegment{
		StartCommit:       commit,
		EndCommit:         commit,
		StartTx:           0,
		EndTx:             0,
		KindedPath:        "",
		LogFile:           string(snapWriter.LogFileID()),
		LogPosition:       snapWriter.EntryPosition(),
		LogFileGeneration: generation,
		ScopeID:           nil,
	}
	s.index.Add(snapSeg)

	// In the log and in the baseline index, so it is readable: advance the watermark.
	// No notification — a schema snapshot carries no patch for a watcher to apply.
	s.tick.publish(commit, nil)

	s.logger.Info("schema snapshot created", "commit", commit, "status", status, "logFile", snapWriter.LogFileID(), "position", snapWriter.EntryPosition())
	return commit, nil
}

// identityChangeAllowed refuses a schema that changes which arrays have an identity in a
// way the stored data cannot follow.
//
// An array that GAINS an identity is the boundary between two regimes: before, it is one
// indexed path whose elements have no names; after, each element is a field. Elements
// already written by position have no names to be found under, so the identity is
// declared before the array is written, or the array is emptied first -- a refusal here,
// where it is a schema error, rather than elements that vanish from every read after
// the migration commit. An array that LOSES an identity is refused for the same reason
// a rename is: the elements it would strand have names and no successor to hold them
// (element_identity.md).
func (s *Storage) identityChangeAllowed(pending *api.Schema) error {
	active := s.schemaForScope(nil)
	for _, p := range active.KeyedPaths() {
		if !pending.Keyed(p) {
			return fmt.Errorf("%q cannot lose its identity %s: its elements are held under their names",
				p, strings.Join(active.Identity(p), ","))
		}
	}
	commit, err := s.GetCurrentCommit()
	if err != nil || commit == 0 {
		return nil
	}
	for _, p := range pending.KeyedPaths() {
		if active.Keyed(p) {
			if !slices.Equal(active.Identity(p), pending.Identity(p)) {
				return fmt.Errorf("%q cannot change its identity from %s to %s: its elements are held under their names",
					p, strings.Join(active.Identity(p), ","), strings.Join(pending.Identity(p), ","))
			}
			continue
		}
		c, err := s.Read(commit, nil, p)
		if err != nil {
			return err
		}
		held, err := collectAll(c)
		if err != nil {
			return err
		}
		if held = ir.Uncomment(held); held != nil && held.Type == ir.ArrayType && len(held.Values) > 0 {
			return fmt.Errorf("%q cannot be given the identity %s: it already holds %d elements written by "+
				"position, which have no names; declare the identity before the array is written, or empty it first",
				p, strings.Join(pending.Identity(p), ","), len(held.Values))
		}
	}
	return nil
}
