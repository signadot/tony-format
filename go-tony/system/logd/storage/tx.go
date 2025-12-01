package storage

import (
	"fmt"
	"sync"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// TxResult represents the result of a completed transaction.
type TxResult struct {
	Committed bool
	Commit    int64 // Commit identifier returned by NextCommit()
	Error     error
}

// Tx represents a transaction in progress.
// Multiple goroutines can safely call AddDiff concurrently on the same transaction.
// The last participant to add a diff will automatically commit the transaction.
type Tx struct {
	storage          *Storage
	txID             int64 // Transaction ID (same as txSeq)
	txSeq            int64
	participantCount int
	committed        bool
	done             chan struct{} // closed when transaction completes
	result           *TxResult
	mu               sync.Mutex // protects committed, done, result
}

// NewTx creates a new transaction with the specified number of participants.
// Returns a transaction that can be used by multiple parallel participants.
//
// Example usage (typical pattern for parallel HTTP handlers):
//
//	// Handler 1 (or any participant)
//	tx, err := storage.NewTx(participantCount)
//	patch := &api.Patch{...}  // Contains path, match, and diff
//	isLast, err := tx.AddDiff(patch)
//	if isLast {
//	    result, err := tx.Commit()  // Last participant commits
//	} else {
//	    result := tx.WaitForCompletion()  // Others wait
//	}
func (s *Storage) NewTx(participantCount int) (*Tx, error) {
	if participantCount < 1 {
		return nil, fmt.Errorf("participantCount must be at least 1, got %d", participantCount)
	}

	txSeq, err := s.NextTxSeq()
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction sequence: %w", err)
	}

	// Transaction ID is the same as txSeq
	txID := txSeq
	state := NewTransactionState(txID, participantCount)

	if err := s.WriteTransactionState(state); err != nil {
		return nil, fmt.Errorf("failed to write transaction state: %w", err)
	}

	return &Tx{
		storage:          s,
		txID:             txID,
		txSeq:            txSeq,
		participantCount: participantCount,
		committed:        false,
		done:             make(chan struct{}),
		result:           nil,
		mu:               sync.Mutex{},
	}, nil
}

// JoinTx allows a participant to join an existing transaction by transaction ID.
// This is the primary way participants coordinate - they all receive the same
// transaction ID and join the same transaction.
//
// Example:
//
//	// Multiple parallel HTTP handlers all receive the same txID
//	tx, err := storage.JoinTx(txID)
//	patch := &api.Patch{...}  // Contains path, match, and diff
//	isLast, err := tx.AddDiff(patch)
//	if isLast {
//	    result, err := tx.Commit()
//	} else {
//	    result := tx.WaitForCompletion()
//	}
func (s *Storage) JoinTx(txID int64) (*Tx, error) {
	// TODO: Implement in Step 3
	return nil, nil
}

// ID returns the transaction ID, useful for sharing with other participants.
func (tx *Tx) ID() int64 {
	return tx.txID
}

// AddDiff adds a pending diff to the transaction and atomically updates the transaction state.
// This method is safe to call concurrently from multiple goroutines.
//
// Returns:
//   - isLastParticipant: true if this participant is the last one (should call Commit)
//   - error: any error that occurred
//
// The caller should check isLastParticipant:
//   - If true: this goroutine should call Commit() to finalize the transaction
//   - If false: this goroutine should call WaitForCompletion() to wait for the last participant
func (tx *Tx) AddDiff(patch *api.Patch) (isLastParticipant bool, err error) {
	// TODO: Implement in Step 4
	return false, nil
}

// Commit commits all pending diffs atomically.
// This should only be called by the last participant (the one for whom AddDiff returned true).
// Other participants should call WaitForCompletion() instead.
//
// This method is idempotent - if called multiple times or after the transaction is already
// committed, it returns the existing result without error.
func (tx *Tx) Commit() (*TxResult, error) {
	// TODO: Implement in Steps 6-8
	return nil, nil
}

// WaitForCompletion waits for the transaction to complete or abort.
// This should be called by participants that are NOT the last one.
// Returns the transaction result once available.
func (tx *Tx) WaitForCompletion() *TxResult {
	// TODO: Implement in Step 9
	return nil
}

// GetResult returns the current result without waiting.
// Returns nil if the transaction hasn't completed yet.
func (tx *Tx) GetResult() *TxResult {
	// TODO: Implement in Step 9
	return nil
}

// Rollback aborts the transaction, deleting all pending diffs and transaction state.
// This should typically only be called on error paths or timeouts.
func (tx *Tx) Rollback() error {
	// TODO: Implement in Step 11
	return nil
}
