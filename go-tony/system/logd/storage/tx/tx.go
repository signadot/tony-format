package tx

import (
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// Tx is the public interface for transaction coordination.
// It provides methods for participants to interact with a transaction.
type Tx interface {
	// ID returns the transaction ID, useful for sharing with other participants.
	ID() int64

	// NewPatcher creates a new patcher handle for this transaction.
	// Each participant should get their own patcher.  If NewPatcher
	// has already added all patches, NewPatcher returns an error.
	NewPatcher(p *api.Patch) (Patcher, error)

	// IsComplete returns true if all expected participants have submitted their patches.
	IsComplete() bool
}

// Patcher is the public interface for a participant's handle to a transaction.
// Multiple goroutines can safely call methods concurrently on patchers for the same transaction.
type Patcher interface {
	// Commit commits all pending diffs atomically.
	// This should only be called by the last participant.
	// Other participants should call WaitForCompletion() instead.
	//
	// This method is idempotent - if called multiple times or after the transaction is already
	// committed, it returns the existing result.
	Commit() *Result
}

// Result represents the result of a transaction commit.
type Result struct {
	Committed bool
	Matched   bool
	Commit    int64 // Commit identifier returned by NextCommit(), 0 if not committed
	Error     error
}

// Store provides storage for active transactions.
// Implementations can be in-memory (for now) or on-disk (for later).
type Store interface {
	// Get retrieves a transaction state by ID, returns nil if not found
	Get(txID int64) (Tx, error)

	// Put stores or updates a transaction state
	Put(Tx) error

	// Delete removes a transaction
	Delete(int64) error

	// List returns all transaction IDs (for recovery/cleanup)
	List() ([]int64, error)
}

// CommitOps provides the operations needed to commit a transaction.
type CommitOps interface {
	// ReadStateAt reads the current state at the given kpath and commit.
	ReadStateAt(kp string, commit int64) (*ir.Node, error)

	// GetCurrentCommit returns the current commit number.
	GetCurrentCommit() (int64, error)

	// NextCommit allocates and returns the next commit number.
	NextCommit() (int64, error)

	// WriteAndIndex writes the transaction entry and indexes the diff.
	// Returns the log file and position where the entry was written.
	WriteAndIndex(commit, txSeq int64, timestamp string, mergedPatch *ir.Node, txState *State, lastCommit int64) (logFile string, pos int64, err error)
}

// State is the structure tracking transaction evolution over time until
// commit
//
//tony:schemagen=tx-state
type State struct {
	TxID        int64          // Transaction ID
	CreatedAt   time.Time      // RFC3339 timestamp
	Timeout     time.Duration  // Maximum time to wait for all participants (0 = no timeout)
	Meta        *api.PatchMeta // Metadata from docd
	PatcherData []*PatcherData // All participant patches
}

// TxPatcher is a participant in a Tx.
//
//tony:schemagen=patcher-data
type PatcherData struct {
	ReceivedAt time.Time
	API        *api.Patch
}
