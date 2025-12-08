package dlog

import (
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// Entry represents a log entry written to logA/logB.
// This structure supports 4 types of entries:
//   - Plain patch: Patch set, TxSource/SnapPos nil *LastCommit=Commit-1
//   - Transaction: Patch and TxSource set, SnapPos nil *LastCommit=Commit-1
//   - Snapshot: SnapPos set to point to log where state exists, TxSource nil,  LastCommit nil
//   - Compaction: Patch set, LastCommit set Commit-*LastCommit > 1, TxSource nil, SnapPos nil
//
//tony:schemagen=entry
type Entry struct {
	Commit     int64     // Commit number (set when appended to log)
	Timestamp  string    // RFC3339 timestamp
	Patch      *ir.Node  // Root patch/diff (always at root, empty kinded path "")
	TxSource   *tx.State // Transaction state (for transaction entries)
	SnapPos    *int64    // Snapshot position (for snapshot entries)
	LastCommit *int64    // Last commit before compaction (for compaction entries)
}

// NewEntry creates a dlog.Entry for a transaction commit.
// The entry contains the merged patch and transaction state for debugging/dev.
// Parameters:
//   - state: The transaction state (will be stored in TxSource)
//   - mergedPatch: The merged root patch/diff (already merged from all participants)
//   - commit: The commit number for this entry
//   - timestamp: RFC3339 timestamp string
//   - lastCommit: The commit number before this one (typically commit-1)
func NewEntry(state *tx.State, mergedPatch *ir.Node, commit int64, timestamp string, lastCommit int64) *Entry {
	return &Entry{
		Commit:     commit,
		Timestamp:  timestamp,
		Patch:      mergedPatch,
		TxSource:   state,
		SnapPos:    nil,
		LastCommit: &lastCommit,
	}
}
