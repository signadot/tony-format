package dlog

import (
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// SchemaEntry is a schema in the log: the one a schema commit sets, or, on a root
// snapshot, the one in force at the snapshot's commit. SetAt is the commit that set it,
// which for a schema commit is the entry's own; a snapshot carries it so a rebuild from a
// compacted log, where the schema commit itself may be gone, still knows when the schema
// took effect (090mbrhsh12ksfr8mhn0).
//
//tony:schemagen=schema-entry
type SchemaEntry struct {
	// Schema is the Tony schema document (nil = schemaless)
	Schema *ir.Node

	// SetAt is the commit the schema was set at.
	SetAt int64
}

// Entry represents a log entry written to logA/logB.
// This structure supports these types of entries:
//   - Transaction: Patch and TxSource set, SnapPos/SchemaEntry nil, *LastCommit=Commit-1
//   - Snapshot: SnapPos set, SnapPath the path it is of (nil for the root), TxSource nil,
//     LastCommit nil; a root snapshot's SchemaEntry is the schema in force at its commit
//   - Schema commit: SchemaEntry set, *LastCommit=Commit-1, SnapPos nil; Patch is the
//     rewrite the change makes to the data, or nil (NewSchemaEntry).
//   - Scope overlay: Patch, ScopeID and ScopeOverlay set, TxSource nil. Nothing writes one;
//     a log that holds them still decodes (see ScopeOverlay).
//
//tony:schemagen=entry
type Entry struct {
	Commit    int64  // Commit number (set when appended to log)
	Timestamp string // RFC3339 timestamp
	// Author is who wrote a transaction entry: the transaction's author
	// (tx.State.Author), kept beside the timestamp so the log answers who as well as
	// when (cn1n32yph12ks5wrmhn0). Empty is none: a write that named no author, a
	// snapshot, a schema commit, or an entry from before this was recorded.
	Author   string
	Patch    *ir.Node  // Root patch/diff (always at root, empty kinded path "")
	TxSource *tx.State // Transaction state (for transaction entries)
	SnapPos  *int64    // Snapshot position (for snapshot entries)
	// SnapPath is the path a snapshot is OF: its event stream is the subtree there, and
	// its index segment sits at that path, which is the only place a read finds it. nil
	// is the root -- the snapshot the switch takes -- and a log written before paths
	// were snapshotted decodes to it (storage/path_snapshot.go).
	SnapPath    *string
	LastCommit  *int64       // The commit a patch entry follows (Commit-1); nil for a snapshot
	ScopeID     *string      // nil = baseline, non-nil = scope-specific data
	SchemaEntry *SchemaEntry // A schema commit's schema, or the one in force at a root snapshot

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

// NewSchemaEntry creates the dlog.Entry of a schema commit: commit takes the next number
// in the one sequence, and from it on the store's schema is schema. rewrite, when not
// nil, is the delta the change makes to the data -- the arrays whose identity changed,
// restated in the new schema's form (storage/schema.go) -- stored and indexed as any
// commit's delta is.
func NewSchemaEntry(schema *ir.Node, rewrite *ir.Node, commit int64, timestamp string, lastCommit int64) *Entry {
	return &Entry{
		Commit:      commit,
		Timestamp:   timestamp,
		Patch:       rewrite,
		LastCommit:  &lastCommit,
		SchemaEntry: &SchemaEntry{Schema: schema, SetAt: commit},
	}
}

// IsSchemaCommit reports whether e is a schema commit: a schema, and not a snapshot.
func (e *Entry) IsSchemaCommit() bool {
	return e.SchemaEntry != nil && e.SnapPos == nil
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
	var author string
	if state != nil {
		author = state.Author
	}
	return &Entry{
		Commit:     commit,
		Timestamp:  timestamp,
		Author:     author,
		Patch:      mergedPatch,
		TxSource:   state,
		SnapPos:    nil,
		LastCommit: &lastCommit,
		ScopeID:    scopeID,
	}
}
