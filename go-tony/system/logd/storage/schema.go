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

	if pendingSchema != nil {
		s.schema.SetPending(pendingSchema, pendingSchemaCommit, api.ParseSchemaFromNode(pendingSchema))
		s.logger.Info("restored pending migration state",
			"activeSchemaCommit", activeSchemaCommit,
			"pendingSchemaCommit", pendingSchemaCommit)
	} else if activeSchema != nil {
		s.logger.Info("restored schema state", "activeSchemaCommit", activeSchemaCommit)
	}

	return nil
}

// StartMigration begins a schema migration by setting a pending schema.
// Returns ErrMigrationInProgress if a migration is already in progress.
// This creates a snapshot with the pending schema, and answers the snapshot's commit.
//
// A schema that does not validate is refused, and so is one that changes an array's
// identity in a way the stored data cannot follow: an array losing or changing its
// identity, or gaining one while it holds elements written by position.
func (s *Storage) StartMigration(schema *ir.Node) (int64, error) {
	unlock := s.lockSchemaChange()
	defer unlock()

	if s.schema.HasPending() {
		return 0, ErrMigrationInProgress
	}

	// Reject a schema that cannot mean what it says BEFORE it is written. Key derivation
	// decides what a stored delta records, and a delta cannot be un-recorded, so an
	// ambiguous schema is caught where it is proposed rather than where it bites.
	parsed := api.ParseSchemaFromNode(schema)
	if err := parsed.Validate(); err != nil {
		return 0, fmt.Errorf("schema cannot be adopted: %w", err)
	}
	if err := s.identityChangeAllowed(parsed); err != nil {
		return 0, fmt.Errorf("schema cannot be adopted: %w", err)
	}

	commit, err := s.createSchemaSnapshot(schema, dlog.SchemaStatusPending)
	if err != nil {
		return 0, err
	}
	s.schema.SetPending(schema, commit, parsed)
	return commit, nil
}

// CompleteMigration completes a pending schema migration.
// Returns ErrNoMigrationInProgress if no migration is in progress.
// This creates a snapshot with the active schema, which applies to every commit after it.
//
// The index is not touched: it holds stored deltas, the same under either schema. A
// pending index built beside it and swapped in here was a copy of it, and everything the
// copy missed -- commits during Start and here, everything before the previous
// migration, every snapshot -- was lost at the swap (090mbrhsh12ksfr8mhn0).
func (s *Storage) CompleteMigration() (int64, error) {
	unlock := s.lockSchemaChange()
	defer unlock()

	pendingSchema, _ := s.schema.GetPending()
	if pendingSchema == nil {
		return 0, ErrNoMigrationInProgress
	}

	// Asked again, here, where it takes effect: the writes since StartMigration were
	// lowered under the active schema, and one of them can have put elements by position
	// into an array this schema gives an identity to.
	if err := s.identityChangeAllowed(s.schema.GetPendingParsed()); err != nil {
		return 0, fmt.Errorf("schema cannot be adopted: %w", err)
	}

	commit, err := s.createSchemaSnapshot(pendingSchema, dlog.SchemaStatusActive)
	if err != nil {
		return 0, err
	}
	s.schema.PromotePending(commit)
	return commit, nil
}

// AbortMigration aborts a pending schema migration.
// Returns ErrNoMigrationInProgress if no migration is in progress.
func (s *Storage) AbortMigration() (int64, error) {
	unlock := s.lockSchemaChange()
	defer unlock()

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

// lockSchemaChange orders a schema change with what it must not interleave with:
// commitMu, which every commit holds from its precondition to its publication, so a
// schema snapshot takes the next commit number with every earlier commit already in the
// index, and no write is lowered under one schema and stored after the other; and snapMu,
// which the inactive log's other writers hold -- the switch, compaction, a path snapshot.
// In that order: nothing holding snapMu waits on a commit (gdpv3fsvh12ksynxmdn0).
func (s *Storage) lockSchemaChange() (unlock func()) {
	s.commitMu.Lock()
	s.snapMu.Lock()
	return func() {
		s.snapMu.Unlock()
		s.commitMu.Unlock()
	}
}

// createSchemaSnapshot creates a snapshot with a schema change entry.
// This is similar to createSnapshot but includes the SchemaEntry.
// Each schema snapshot gets its own commit number to avoid duplicates at the same commit.
// The caller holds lockSchemaChange.
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
		c, err := s.Read(commit, nil, p)
		if err != nil {
			return err
		}
		held, err := collectAll(c)
		if err != nil {
			return err
		}
		held = ir.Uncomment(held)
		if active.Keyed(p) {
			// Held under their names, as an object of them: a change strands every one.
			// An array holding none strands nothing -- and the store holding a commit is
			// not the array holding an element, since a schema change is a commit too.
			if !slices.Equal(active.Identity(p), pending.Identity(p)) && held != nil && held.Type == ir.ObjectType && len(held.Fields) > 0 {
				return fmt.Errorf("%q cannot change its identity from %s to %s: its elements are held under their names",
					p, strings.Join(active.Identity(p), ","), strings.Join(pending.Identity(p), ","))
			}
			continue
		}
		if held != nil && held.Type == ir.ArrayType && len(held.Values) > 0 {
			return fmt.Errorf("%q cannot be given the identity %s: it already holds %d elements written by "+
				"position, which have no names; declare the identity before the array is written, or empty it first",
				p, strings.Join(pending.Identity(p), ","), len(held.Values))
		}
	}
	return nil
}
