package dlog

import (
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// Schema status constants
const (
	SchemaStatusPending = "pending" // Start migration, creates pending index
	SchemaStatusActive  = "active"  // Complete migration or set initial schema
	SchemaStatusAborted = "aborted" // Cancel migration, discard pending index
)

// SchemaEntry represents a schema change in the log.
// Schema changes always occur at snapshot boundaries.
//
//tony:schemagen=schema-entry
type SchemaEntry struct {
	// Schema is the Tony schema document (nil = schemaless)
	Schema *ir.Node

	// Status is one of SchemaStatusPending, SchemaStatusActive, or SchemaStatusAborted
	Status string
}

// Entry represents a log entry written to logA/logB.
// This structure supports 5 types of entries:
//   - Plain patch: Patch set, TxSource/SnapPos/SchemaEntry nil, *LastCommit=Commit-1
//   - Transaction: Patch and TxSource set, SnapPos/SchemaEntry nil, *LastCommit=Commit-1
//   - Snapshot: SnapPos set, TxSource nil, LastCommit nil, SchemaEntry nil
//   - Compaction: Patch set, LastCommit set Commit-*LastCommit > 1, TxSource/SchemaEntry nil
//   - Schema change: SchemaEntry set, SnapPos set (schema changes require snapshot)
//
//tony:schemagen=entry
type Entry struct {
	Commit      int64        // Commit number (set when appended to log)
	Timestamp   string       // RFC3339 timestamp
	Patch       *ir.Node     // Root patch/diff (always at root, empty kinded path "")
	TxSource    *tx.State    // Transaction state (for transaction entries)
	SnapPos     *int64       // Snapshot position (for snapshot entries)
	LastCommit  *int64       // Last commit before compaction (for compaction entries)
	ScopeID     *string      // nil = baseline, non-nil = scope-specific data
	SchemaEntry *SchemaEntry // Schema change (always with SnapPos for snapshot)
}

// NewEntry creates a dlog.Entry for a transaction commit.
// The entry contains the merged patch and transaction state for debugging/dev.
// Parameters:
//   - state: The transaction state (will be stored in TxSource)
//   - mergedPatch: The merged root patch/diff (already merged from all participants)
//   - commit: The commit number for this entry
//   - timestamp: RFC3339 timestamp string
//   - lastCommit: The commit number before this one (typically commit-1)
//   - scopeID: nil for baseline, non-nil for scope-specific data
func NewEntry(state *tx.State, mergedPatch *ir.Node, commit int64, timestamp string, lastCommit int64, scopeID *string) *Entry {
	return &Entry{
		Commit:     commit,
		Timestamp:  timestamp,
		Patch:      mergedPatch,
		TxSource:   state,
		SnapPos:    nil,
		LastCommit: &lastCommit,
		ScopeID:    scopeID,
	}
}
