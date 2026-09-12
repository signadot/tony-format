package tx

import (
	"fmt"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// Tx is the public interface for transaction coordination.
// It provides methods for participants to interact with a transaction.
type Tx interface {
	// ID returns the transaction ID, useful for sharing with other participants.
	ID() int64

	// CreatedAt returns when the transaction was created.
	CreatedAt() time.Time

	// Timeout returns the configured timeout for this transaction.
	Timeout() time.Duration

	// Scope returns the scope for this transaction.
	// All participants must have the same scope.
	Scope() *string

	// NewPatcher creates a new patcher handle for this transaction.
	// Each participant should get their own patcher.  If NewPatcher
	// has already added all patches, NewPatcher returns an error.
	NewPatcher(p *api.Patch) (Patcher, error)

	// IsComplete returns true if all expected participants have submitted their patches.
	IsComplete() bool

	// Expire marks the transaction as expired, notifying any waiting participants.
	// Called by the store's cleanup routine before deleting the transaction.
	Expire()
}

// Patcher is the public interface for a participant's handle to a transaction.
// Multiple goroutines can safely call methods concurrently on patchers for the same transaction.
type Patcher interface {
	// Commit commits all pending diffs atomically.
	// Every participant calls it. It blocks until all participants have joined (or
	// the transaction times out or expires, which bounds every wait -- a transaction
	// always has a timeout); the first to arrive performs the commit,
	// and every participant receives the same outcome.
	//
	// This method is idempotent - if called multiple times or after the transaction is already
	// committed, it returns the existing result.
	Commit() *Result
}

// Result represents the result of a transaction commit.
//
// Matched false with a nil Error is a precondition that did not hold: nothing was
// written, and it is not a failure.
type Result struct {
	Committed bool     // The patch was stored, indexed and published
	Matched   bool     // Every precondition held
	Commit    int64    // Commit identifier returned by NextCommit(), 0 if not committed
	Data      *ir.Node // This participant's patch data as committed: auto-generated IDs injected, keyed arrays in the stored form; nil unless Committed
	Error     error
}

// Store provides storage for active transactions.
// InMemoryTxStore is the implementation the storage package uses.
type Store interface {
	// Get retrieves a transaction state by ID, returns nil if not found
	Get(txID int64) (Tx, error)

	// Put stores or updates a transaction state
	Put(Tx) error

	// Delete removes a transaction
	Delete(int64) error

	// List returns all transaction IDs (for recovery/cleanup)
	List() ([]int64, error)

	// Close stops any background goroutines and releases resources
	Close()
}

// CommitOps provides the operations needed to commit a transaction.
type CommitOps interface {
	// StateAt reads the value at kp as of commit, in the view scopeID names, as the
	// store holds it: one bounded read at that path, under the store's write budget.
	// It serves a precondition, which has been lowered to the same form, and the
	// checks the write path makes on an array a positional write names. Nil is absent.
	//
	// The result is READ-ONLY: navigate it and match it; do not mutate it.
	StateAt(kp string, commit int64, scopeID *string) (*ir.Node, error)

	// GetCurrentCommit returns the current commit number.
	GetCurrentCommit() (int64, error)

	// NextCommit allocates and returns the next commit number.
	NextCommit() (int64, error)

	// GetSchema returns the schema for the given scope.
	GetSchema(scopeID *string) *api.Schema

	// WriteAndIndex writes the transaction entry, indexes the diff, and publishes the
	// commit — makes it readable and queues its notification. Returns the log file and
	// position where the entry was written.
	//
	// Publishing belongs here, under the commit lock, because that is what orders it:
	// the watermark advances and the notification is queued in commit order, while the
	// fan-out itself runs elsewhere so a slow consumer cannot stall a commit. It used to
	// be a separate Notify the caller fired after unlocking, which left the order of two
	// concurrent commits to the goroutine scheduler.
	WriteAndIndex(commit, txSeq int64, timestamp string, mergedPatch *ir.Node, txState *State, lastCommit int64) (logFile string, pos int64, err error)

	// LockCommit acquires the storage-wide commit lock and returns its release func. The
	// CAS-precondition evaluation, NextCommit, and WriteAndIndex must all run under it so the
	// compare-and-swap is atomic w.r.t. other commits (no lost update).
	LockCommit() (release func())
}

// State is the structure tracking transaction evolution over time until
// commit
//
//tony:schemagen=tx-state
type State struct {
	TxID        int64          // Transaction ID
	CreatedAt   time.Time      // RFC3339 timestamp
	Timeout     time.Duration  // Maximum time to wait for all participants; New makes 0 DefaultTimeout
	Scope       *string        // Scope for this transaction (nil = baseline)
	PatcherData []*PatcherData // All participant patches
}

// Author is the transaction's writer: the author its participants named, which is one
// author, since a participant naming another is refused at the join
// (AuthorMismatchError). Empty when they named none, or none has joined.
func (st *State) Author() string {
	for _, pd := range st.PatcherData {
		if pd != nil && pd.API != nil {
			return pd.API.Author
		}
	}
	return ""
}

// AuthorMismatchError refuses a participant whose author is not the transaction's. A
// commit has one author; a transaction whose participants disagreed would be recorded
// under none, which says less than the log knows (api.ErrCodeTxAuthorMismatch).
type AuthorMismatchError struct {
	TxID         int64
	Transactions string // the author the transaction's participants named
	Participant  string // the one this participant named
}

func (e *AuthorMismatchError) Error() string {
	return fmt.Sprintf("transaction %d is written by %q, this participant by %q: a commit has one author",
		e.TxID, e.Transactions, e.Participant)
}

// PatcherData is one participant's contribution to a Tx: its patch, and when it arrived.
// The patch carries the participant's author (api.Patch.Author), so the log's record of
// the transaction says who wrote each part.
//
//tony:schemagen=patcher-data
type PatcherData struct {
	ReceivedAt time.Time
	API        *api.Patch
}
