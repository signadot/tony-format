package dlog

import (
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// Schema status constants
const (
	SchemaStatusPending = "pending" // Start migration: the schema proposed
	SchemaStatusActive  = "active"  // Complete migration or set initial schema
	SchemaStatusAborted = "aborted" // Cancel migration: the proposal dropped
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
// This structure supports these types of entries:
//   - Transaction: Patch and TxSource set, SnapPos/SchemaEntry nil, *LastCommit=Commit-1
//   - Snapshot: SnapPos set, SnapPath the path it is of (nil for the root), TxSource nil,
//     LastCommit nil, SchemaEntry nil
//   - Schema change: SchemaEntry set, SnapPos set (schema changes require snapshot)
//   - Scope overlay: Patch, ScopeID and ScopeOverlay set, TxSource nil. Nothing writes one;
//     a log that holds them still decodes (see ScopeOverlay).
//
//tony:schemagen=entry
type Entry struct {
	Commit    int64     // Commit number (set when appended to log)
	Timestamp string    // RFC3339 timestamp
	Patch     *ir.Node  // Root patch/diff (always at root, empty kinded path "")
	TxSource  *tx.State // Transaction state (for transaction entries)
	SnapPos   *int64    // Snapshot position (for snapshot entries)
	// SnapPath is the path a snapshot is OF: its event stream is the subtree there, and
	// its index segment sits at that path, which is the only place a read finds it. nil
	// is the root -- the snapshot the switch takes -- and a log written before paths
	// were snapshotted decodes to it (storage/path_snapshot.go).
	SnapPath    *string
	LastCommit  *int64       // The commit a patch entry follows (Commit-1); nil for a snapshot
	ScopeID     *string      // nil = baseline, non-nil = scope-specific data
	SchemaEntry *SchemaEntry // Schema change (always with SnapPos for snapshot)

	// ScopeOverlay marks an entry as a scope's materialized ownership rather than one of
	// its writes: an overlay, which logd once wrote beside a baseline snapshot and no
	// longer writes. The two are otherwise indistinguishable -- both are scope-tagged
	// patch entries -- and confusing them is not cosmetic: an overlay SUBSUMES the patches
	// before it, so replaying it as an ordinary patch applies it twice, and the read path
	// must exclude it from the layer it is the base of.
	//
	// It lives on the entry rather than being inferred from the index because the index
	// is rebuildable and the log is the record: index.Build takes the tx from TxSource,
	// and an overlay has none.
	ScopeOverlay bool
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
