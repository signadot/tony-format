package storage

import (
	"fmt"
	"sync"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	// TODO: When match evaluation is implemented, add:
	// "github.com/signadot/tony-format/go-tony"
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
	state, err := s.ReadTransactionState(txID)
	if err != nil {
		return nil, fmt.Errorf("failed to read transaction state: %w", err)
	}

	// Validate transaction is still pending
	if state.Status != "pending" {
		return nil, fmt.Errorf("transaction %d is %s, cannot join", txID, state.Status)
	}

	// Transaction ID is the same as txSeq
	txSeq := txID

	return &Tx{
		storage:          s,
		txID:             txID,
		txSeq:            txSeq,
		participantCount: state.ParticipantCount,
		committed:        false,
		done:             make(chan struct{}),
		result:           nil,
		mu:               sync.Mutex{},
	}, nil
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
	// Check if transaction already committed
	tx.mu.Lock()
	if tx.committed {
		tx.mu.Unlock()
		return false, fmt.Errorf("transaction %d already committed", tx.txID)
	}
	tx.mu.Unlock()

	// Extract components from patch
	virtualPath := patch.Body.Path
	if virtualPath == "" {
		return false, fmt.Errorf("patch.Body.Path is required")
	}
	if patch.Body.Patch == nil {
		return false, fmt.Errorf("patch.Body.Patch is required")
	}
	match := patch.Body.Match // May be nil if no match condition

	// Use the transaction's txSeq (same as txID) for all diffs in this transaction
	// Don't write file yet - wait for match evaluation in Commit()

	// Atomically update transaction state and check if we're the last participant
	var lastParticipant bool
	err = tx.storage.UpdateTransactionState(tx.txID, func(currentState *TransactionState) {
		currentState.ParticipantsReceived++
		currentState.Diffs = append(currentState.Diffs, PendingDiff{
			Path:      virtualPath,
			DiffFile:  "", // Will be set when file is written after match evaluation
			WrittenAt: "", // Will be set when file is written after match evaluation
		})
		// Store the full patch in ParticipantRequests (contains the diff content)
		currentState.ParticipantRequests = append(currentState.ParticipantRequests, patch)
		// Store match condition if present
		if match != nil && match.Type != ir.NullType {
			// Store as api.Match structure
			matchReq := &api.Match{
				Body: api.Body{
					Path:  virtualPath,
					Match: match,
				},
			}
			currentState.ParticipantMatches = append(currentState.ParticipantMatches, matchReq)
		}
		lastParticipant = currentState.ParticipantsReceived >= currentState.ParticipantCount
	})

	if err != nil {
		return false, fmt.Errorf("failed to update transaction state: %w", err)
	}

	return lastParticipant, nil
}

// Commit commits all pending diffs atomically.
// This should only be called by the last participant (the one for whom AddDiff returned true).
// Other participants should call WaitForCompletion() instead.
//
// This method is idempotent - if called multiple times or after the transaction is already
// committed, it returns the existing result without error.
//
// Commit flow:
// 1. Check if already committed (idempotent)
// 2. Read transaction state
// 3. Evaluate all match conditions atomically
// 4. If any match fails → abort transaction (delete state, set error result)
// 5. If all matches pass → write pending files, commit them, create log entry, set success result
func (tx *Tx) Commit() (*TxResult, error) {
	tx.mu.Lock()
	// Idempotent: if already committed, return existing result
	if tx.committed {
		result := tx.result
		tx.mu.Unlock()
		return result, nil
	}
	tx.mu.Unlock()

	// Read current transaction state
	state, err := tx.storage.ReadTransactionState(tx.txID)
	if err != nil {
		return tx.setResult(false, 0, fmt.Errorf("failed to read transaction state: %w", err))
	}

	// Validate we have all participants
	if state.ParticipantsReceived < state.ParticipantCount {
		return tx.setResult(false, 0, fmt.Errorf("transaction %d: only %d/%d participants received", tx.txID, state.ParticipantsReceived, state.ParticipantCount))
	}

	// Step 3: Evaluate all match conditions atomically
	// TODO: The interface for reading current document state is still being finalized
	// due to recent compaction work. Once finalized, replace this placeholder with:
	//   - For each match in state.ParticipantMatches:
	//     - Read current state: currentState, err := tx.storage.ReadCurrentState(match.Body.Path)
	//     - Evaluate: matched, err := tony.Match(currentState, match.Body.Match)
	//     - If !matched → abort transaction
	//
	// For now, we'll skip match evaluation and proceed with commit.
	// This allows the file writing logic to be tested independently.
	//
	// When match evaluation is implemented, uncomment the following:
	/*
	for i, matchReq := range state.ParticipantMatches {
		// Read current committed state for this path
		currentState, err := tx.storage.ReadCurrentState(matchReq.Body.Path)
		if err != nil {
			return tx.setResult(false, 0, fmt.Errorf("failed to read current state for path %q: %w", matchReq.Body.Path, err))
		}

		// Evaluate match condition
		matched, err := tony.Match(currentState, matchReq.Body.Match)
		if err != nil {
			return tx.setResult(false, 0, fmt.Errorf("match evaluation error for path %q: %w", matchReq.Body.Path, err))
		}
		if !matched {
			// Match failed - abort transaction
			if err := tx.abortTransaction(state, fmt.Errorf("match condition failed for path %q", matchReq.Body.Path)); err != nil {
				return tx.setResult(false, 0, fmt.Errorf("failed to abort transaction: %w", err))
			}
			return tx.result, nil
		}
	}
	*/

	// Step 4: All matches passed (or no matches) - proceed with commit
	// Allocate commit number
	commit, err := tx.storage.NextCommit()
	if err != nil {
		return tx.setResult(false, 0, fmt.Errorf("failed to allocate commit number: %w", err))
	}

	// Step 5: Write pending files and commit them
	timestamp := time.Now().UTC().Format(time.RFC3339)
	pendingFileRefs := make([]PendingFileRef, 0, len(state.Diffs))

	for i, diff := range state.Diffs {
		patch := state.ParticipantRequests[i]
		virtualPath := diff.Path
		txSeq := state.TransactionID // Use transaction ID (same for all diffs)

		// Write the pending file (using WriteDiff with commit=0 for pending)
		// Note: We use WriteDiff (not WriteDiffAtomically) because we already have the txSeq
		// and commit number allocated. We're writing as pending first, then committing.
		err := tx.storage.WriteDiff(virtualPath, 0, txSeq, timestamp, patch.Body.Patch, true)
		if err != nil {
			// Failed to write - abort transaction
			if abortErr := tx.abortTransaction(state, fmt.Errorf("failed to write pending file for path %q: %w", virtualPath, err)); abortErr != nil {
				return tx.setResult(false, 0, fmt.Errorf("failed to abort after write error: %w", abortErr))
			}
			return tx.result, nil
		}

		// Update diff entry with file path and timestamp
		state.Diffs[i].WrittenAt = timestamp
		// Note: DiffFile path will be set when we commit the pending file

		// Commit the pending file (rename .pending to .diff and update index)
		err = tx.storage.CommitPendingDiff(virtualPath, txSeq, commit)
		if err != nil {
			// Failed to commit - abort transaction
			if abortErr := tx.abortTransaction(state, fmt.Errorf("failed to commit pending file for path %q: %w", virtualPath, err)); abortErr != nil {
				return tx.setResult(false, 0, fmt.Errorf("failed to abort after commit error: %w", abortErr))
			}
			return tx.result, nil
		}

		// Track pending file reference for transaction log
		pendingFileRefs = append(pendingFileRefs, PendingFileRef{
			VirtualPath: virtualPath,
			TxSeq:       txSeq,
		})
	}

	// Step 6: Create transaction log entry
	logEntry := &TransactionLogEntry{
		Commit:        commit,
		TransactionID: tx.txID,
		Timestamp:     timestamp,
		PendingFiles:  pendingFileRefs,
	}
	if err := tx.storage.AppendTransactionLog(logEntry); err != nil {
		// Failed to write log - this is non-fatal but we should still abort
		// since we can't guarantee recovery
		if abortErr := tx.abortTransaction(state, fmt.Errorf("failed to write transaction log: %w", err)); abortErr != nil {
			return tx.setResult(false, 0, fmt.Errorf("failed to abort after log write error: %w", abortErr))
		}
		return tx.result, nil
	}

	// Step 7: Update transaction state to "committed" and delete state file
	if err := tx.storage.UpdateTransactionState(tx.txID, func(currentState *TransactionState) {
		currentState.Status = "committed"
	}); err != nil {
		// Non-fatal - transaction is already committed, just log it
		tx.storage.log.Error("failed to update transaction state to committed", "txID", tx.txID, "error", err)
	}
	// Delete transaction state file (transaction is complete)
	if err := tx.storage.DeleteTransactionState(tx.txID); err != nil {
		// Non-fatal - transaction is already committed
		tx.storage.log.Error("failed to delete transaction state file", "txID", tx.txID, "error", err)
	}

	// Step 8: Set success result and signal completion
	return tx.setResult(true, commit, nil)
}

// abortTransaction aborts the transaction by deleting pending files and updating state.
// This is called when match evaluation fails or when file operations fail.
func (tx *Tx) abortTransaction(state *TransactionState, abortErr error) error {
	// Delete all pending files that were written
	txSeq := state.TransactionID // Use transaction ID (same for all diffs)
	for _, diff := range state.Diffs {
		if diff.DiffFile != "" || diff.WrittenAt != "" {
			// File was written, delete it
			if err := tx.storage.DeletePendingDiff(diff.Path, txSeq); err != nil {
				tx.storage.log.Error("failed to delete pending file during abort", "path", diff.Path, "txSeq", txSeq, "error", err)
			}
		}
		// Note: If DiffFile is empty, no file was written yet, so nothing to delete
	}

	// Update state to "aborted"
	if err := tx.storage.UpdateTransactionState(tx.txID, func(currentState *TransactionState) {
		currentState.Status = "aborted"
	}); err != nil {
		return fmt.Errorf("failed to update transaction state to aborted: %w", err)
	}

	// Delete transaction state file
	if err := tx.storage.DeleteTransactionState(tx.txID); err != nil {
		return fmt.Errorf("failed to delete transaction state file: %w", err)
	}

	return nil
}

// setResult sets the transaction result and signals completion.
// This is thread-safe and idempotent.
func (tx *Tx) setResult(committed bool, commit int64, err error) (*TxResult, error) {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	// Idempotent: if result already set, return it
	if tx.result != nil {
		return tx.result, nil
	}

	tx.result = &TxResult{
		Committed: committed,
		Commit:    commit,
		Error:     err,
	}
	tx.committed = true

	// Signal completion to waiting goroutines
	close(tx.done)

	return tx.result, nil
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

