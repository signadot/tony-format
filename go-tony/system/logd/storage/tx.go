package storage

import (
	"fmt"
	"sync"
	"time"

	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// TxResult represents the result of a completed transaction.
type TxResult struct {
	Committed bool
	Commit    int64 // Commit identifier returned by NextCommit()
	Error     error
}

// Tx represents a multi-participant transaction.
// Each participant gets their own TxPatcher via NewPatcher().
type Tx struct {
	storage          *Storage
	txID             int64 // Transaction ID (same as txSeq)
	txSeq            int64
	participantCount int
}

// TxPatcher is a participant's handle to a transaction.
// Multiple goroutines can safely call AddPatch concurrently on patchers for the same transaction.
// The last participant to add a patch will automatically commit the transaction.
type TxPatcher struct {
	tx        *Tx
	committed bool
	done      chan struct{} // closed when transaction completes
	result    *TxResult
	mu        sync.Mutex // protects committed, done, result
}

// NewTx creates a new transaction with the specified number of participants.
// Returns a transaction that participants can get via GetTx or get a patcher via NewPatcher().
//
// Example usage (typical pattern for parallel HTTP handlers):
//
//	// Create transaction
//	tx, err := storage.NewTx(participantCount)
//	if err != nil {
//	    // handle error
//	}
//
//	// Each participant gets their own patcher
//	patcher := tx.NewPatcher()
//	patch := &api.Patch{...}  // Contains path, match, and diff
//	isLast, err := patcher.AddPatch(patch)
//	if isLast {
//	    result := patcher.Commit()  // Last participant commits
//	} else {
//	    result := patcher.WaitForCompletion()  // Others wait
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
	state := NewTxState(txID, participantCount)

	if err := s.WriteTxState(state); err != nil {
		return nil, fmt.Errorf("failed to write transaction state: %w", err)
	}

	return &Tx{
		storage:          s,
		txID:             txID,
		txSeq:            txSeq,
		participantCount: participantCount,
	}, nil
}

// GetTx gets an existing transaction by transaction ID.
// This is the primary way participants coordinate - they all receive the same
// transaction ID and get the same transaction.
//
// Example:
//
//	// Multiple parallel HTTP handlers all receive the same txID
//	tx, err := storage.GetTx(txID)
//	if err != nil {
//	    // handle error
//	}
//
//	// Each participant gets their own patcher
//	patcher := tx.NewPatcher()
//	patch := &api.Patch{...}  // Contains path, match, and diff
//	isLast, err := patcher.AddPatch(patch)
//	if isLast {
//	    result := patcher.Commit()
//	} else {
//	    result := patcher.WaitForCompletion()
//	}
func (s *Storage) GetTx(txID int64) (*Tx, error) {
	state, err := s.ReadTxState(txID)
	if err != nil {
		return nil, fmt.Errorf("failed to read transaction state: %w", err)
	}

	// Validate transaction is still pending
	if state.Status != "pending" {
		return nil, fmt.Errorf("transaction %d is %s, cannot get", txID, state.Status)
	}

	// Transaction ID is the same as txSeq
	txSeq := txID

	return &Tx{
		storage:          s,
		txID:             txID,
		txSeq:            txSeq,
		participantCount: state.ParticipantCount,
	}, nil
}

// ID returns the transaction ID, useful for sharing with other participants.
func (tx *Tx) ID() int64 {
	return tx.txID
}

// NewPatcher creates a new patcher handle for this transaction.
// Each participant should get their own patcher.
func (tx *Tx) NewPatcher() *TxPatcher {
	return &TxPatcher{
		tx:        tx,
		committed: false,
		done:      make(chan struct{}),
		result:    nil,
		mu:        sync.Mutex{},
	}
}

// AddPatch adds a pending diff to the transaction and atomically updates the transaction state.
// This method is safe to call concurrently from multiple goroutines.
//
// Returns:
//   - isLastParticipant: true if this participant is the last one (should call Commit)
//   - error: any error that occurred
//
// The caller should check isLastParticipant:
//   - If true: this goroutine should call Commit() to finalize the transaction
//   - If false: this goroutine should call WaitForCompletion() to wait for the last participant
func (p *TxPatcher) AddPatch(patch *api.Patch) (isLastParticipant bool, err error) {
	// Check if transaction already committed
	p.mu.Lock()
	if p.committed {
		p.mu.Unlock()
		return false, fmt.Errorf("transaction %d already committed", p.tx.txID)
	}
	p.mu.Unlock()

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
	err = p.tx.storage.UpdateTxState(p.tx.txID, func(currentState *TxState) {
		currentState.ParticipantsReceived++
		currentState.FileMetas = append(currentState.FileMetas, FileMeta{
			Path:      virtualPath,
			FSPath:    "", // Will be set when file is written after match evaluation
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
// This should only be called by the last participant (the one for whom AddPatch returned true).
// Other participants should call WaitForCompletion() instead.
//
// This method is idempotent - if called multiple times or after the transaction is already
// committed, it returns the existing result.
//
// Commit flow:
// 1. Check if already committed (idempotent)
// 2. Read transaction state
// 3. Evaluate all match conditions atomically
// 4. If any match fails → abort transaction (delete state, set error result)
// 5. If all matches pass → write pending files, commit them, create log entry, set success result
//
// Errors are returned in TxResult.Error, not as a separate error return.
func (p *TxPatcher) Commit() *TxResult {
	p.mu.Lock()
	// Idempotent: if already committed, return existing result
	if p.committed {
		result := p.result
		p.mu.Unlock()
		return result
	}
	p.mu.Unlock()

	// Read current transaction state
	state, err := p.tx.storage.ReadTxState(p.tx.txID)
	if err != nil {
		return p.setResult(false, 0, fmt.Errorf("failed to read transaction state: %w", err))
	}

	// Validate we have all participants
	if state.ParticipantsReceived < state.ParticipantCount {
		return p.setResult(false, 0, fmt.Errorf("transaction %d: only %d/%d participants received", p.tx.txID, state.ParticipantsReceived, state.ParticipantCount))
	}

	// Step 3: Evaluate all match conditions atomically
	// For each match condition, read the current committed state and evaluate it
	for _, matchReq := range state.ParticipantMatches {
		// Read current committed state for this path
		currentState, err := p.tx.storage.ReadCurrentState(matchReq.Body.Path)
		if err != nil {
			return p.setResult(false, 0, fmt.Errorf("failed to read current state for path %q: %w", matchReq.Body.Path, err))
		}

		// Evaluate match condition using tony.Match
		// Note: We need to import "github.com/signadot/tony-format/go-tony" for tony.Match
		matched, err := tony.Match(currentState, matchReq.Body.Match)
		if err != nil {
			return p.setResult(false, 0, fmt.Errorf("match evaluation error for path %q: %w", matchReq.Body.Path, err))
		}
		if !matched {
			// Match failed - abort transaction
			if err := p.abortTransaction(state, fmt.Errorf("match condition failed for path %q", matchReq.Body.Path)); err != nil {
				return p.setResult(false, 0, fmt.Errorf("failed to abort transaction: %w", err))
			}
			return p.result
		}
	}

	// Step 4: All matches passed (or no matches) - proceed with commit
	// Allocate commit number
	commit, err := p.tx.storage.NextCommit()
	if err != nil {
		return p.setResult(false, 0, fmt.Errorf("failed to allocate commit number: %w", err))
	}

	// Step 5: Write pending files and commit them
	timestamp := time.Now().UTC().Format(time.RFC3339)
	pendingFileRefs := make([]FileRef, 0, len(state.FileMetas))

	for i, diff := range state.FileMetas {
		patch := state.ParticipantRequests[i]
		virtualPath := diff.Path
		txSeq := p.tx.txSeq // Use transaction sequence from struct

		// Write the pending file (using WriteDiff with commit=0 for pending)
		// Note: We use WriteDiff (not WriteDiffAtomically) because we already have the txSeq
		// and commit number allocated. We're writing as pending first, then committing.
		err := p.tx.storage.WriteDiff(virtualPath, 0, txSeq, timestamp, patch.Body.Patch, true)
		if err != nil {
			// Failed to write - abort transaction
			if abortErr := p.abortTransaction(state, fmt.Errorf("failed to write pending file for path %q: %w", virtualPath, err)); abortErr != nil {
				return p.setResult(false, 0, fmt.Errorf("failed to abort after write error: %w", abortErr))
			}
			return p.result
		}

		// Update diff entry with file path and timestamp
		state.FileMetas[i].WrittenAt = timestamp
		// Note: FSPath will be set when we commit the pending file

		// Commit the pending file (rename .pending to .diff and update index)
		err = p.tx.storage.commit(virtualPath, txSeq, commit)
		if err != nil {
			// Failed to commit - abort transaction
			if abortErr := p.abortTransaction(state, fmt.Errorf("failed to commit pending file for path %q: %w", virtualPath, err)); abortErr != nil {
				return p.setResult(false, 0, fmt.Errorf("failed to abort after commit error: %w", abortErr))
			}
			return p.result
		}

		// Track pending file reference for transaction log
		pendingFileRefs = append(pendingFileRefs, FileRef{
			VirtualPath: virtualPath,
			TxSeq:       txSeq,
		})
	}

	// Step 6: Create transaction log entry
	logEntry := &TxLogEntry{
		Commit:       commit,
		TxID:         p.tx.txID,
		Timestamp:    timestamp,
		PendingFiles: pendingFileRefs,
	}
	if err := p.tx.storage.AppendTxLog(logEntry); err != nil {
		// Failed to write log - this is non-fatal but we should still abort
		// since we can't guarantee recovery
		if abortErr := p.abortTransaction(state, fmt.Errorf("failed to write transaction log: %w", err)); abortErr != nil {
			return p.setResult(false, 0, fmt.Errorf("failed to abort after log write error: %w", abortErr))
		}
		return p.result
	}

	// Step 7: Update transaction state to "committed" and delete state file
	if err := p.tx.storage.UpdateTxState(p.tx.txID, func(currentState *TxState) {
		currentState.Status = "committed"
	}); err != nil {
		// Non-fatal - transaction is already committed, just log it
		p.tx.storage.log.Error("failed to update transaction state to committed", "txID", p.tx.txID, "error", err)
	}
	// Delete transaction state file (transaction is complete)
	if err := p.tx.storage.DeleteTxState(p.tx.txID); err != nil {
		// Non-fatal - transaction is already committed
		p.tx.storage.log.Error("failed to delete transaction state file", "txID", p.tx.txID, "error", err)
	}

	// Step 8: Set success result and signal completion
	return p.setResult(true, commit, nil)
}

// abortTransaction aborts the transaction by deleting pending files and updating state.
// This is called when match evaluation fails or when file operations fail.
func (p *TxPatcher) abortTransaction(state *TxState, abortErr error) error {
	// Delete all pending files that were written
	txSeq := p.tx.txSeq // Use transaction sequence from struct
	for _, diff := range state.FileMetas {
		if diff.FSPath != "" || diff.WrittenAt != "" {
			// File was written, delete it
			if err := p.tx.storage.deletePathAt(diff.Path, txSeq); err != nil {
				p.tx.storage.log.Error("failed to delete pending file during abort", "path", diff.Path, "txSeq", txSeq, "error", err)
			}
		}
		// Note: If FSPath is empty, no file was written yet, so nothing to delete
	}

	// Update state to "aborted"
	if err := p.tx.storage.UpdateTxState(p.tx.txID, func(currentState *TxState) {
		currentState.Status = "aborted"
	}); err != nil {
		return fmt.Errorf("failed to update transaction state to aborted: %w", err)
	}

	// Delete transaction state file
	if err := p.tx.storage.DeleteTxState(p.tx.txID); err != nil {
		return fmt.Errorf("failed to delete transaction state file: %w", err)
	}

	return nil
}

// setResult sets the transaction result and signals completion.
// This is thread-safe and idempotent.
func (p *TxPatcher) setResult(committed bool, commit int64, err error) *TxResult {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Idempotent: if result already set, return it
	if p.result != nil {
		return p.result
	}

	p.result = &TxResult{
		Committed: committed,
		Commit:    commit,
		Error:     err,
	}
	p.committed = true

	// Signal completion to waiting goroutines
	close(p.done)

	return p.result
}

// WaitForCompletion waits for the transaction to complete or abort.
// This should be called by participants that are NOT the last one.
// Returns the transaction result once available.
//
// This method blocks until the transaction completes (either successfully committed
// or aborted). The transaction completes when:
// - The last participant calls Commit() (success or failure)
// - An error occurs during commit
//
// All waiting participants will receive the same result.
func (p *TxPatcher) WaitForCompletion() *TxResult {
	// First check if this instance already has the result
	p.mu.Lock()
	if p.result != nil {
		result := p.result
		p.mu.Unlock()
		return result
	}
	p.mu.Unlock()

	// Poll transaction state until it's no longer pending
	// This works across multiple TxPatcher instances for the same transaction
	for {
		state, err := p.tx.storage.ReadTxState(p.tx.txID)
		if err != nil {
			// Transaction state file doesn't exist - check if transaction was committed
			// (Commit() deletes the state file after writing to log)
			entries, logErr := p.tx.storage.ReadTxLog(nil)
			if logErr == nil {
				for _, entry := range entries {
					if entry.TxID == p.tx.txID {
						// Found in log - transaction was committed
						result := &TxResult{
							Committed: true,
							Commit:    entry.Commit,
							Error:     nil,
						}
						// Cache it for this instance
						p.mu.Lock()
						if p.result == nil {
							p.result = result
							p.committed = true
							close(p.done)
						}
						result = p.result
						p.mu.Unlock()
						return result
					}
				}
			}
			// File doesn't exist and not in log - might be transient error or still committing
			// Check if we have a cached result first
			p.mu.Lock()
			result := p.result
			p.mu.Unlock()
			if result != nil {
				return result
			}
			// No result yet - wait a bit and check again
			select {
			case <-p.done:
				p.mu.Lock()
				result := p.result
				p.mu.Unlock()
				return result
			case <-time.After(10 * time.Millisecond):
				continue
			}
		}

		// If transaction is committed or aborted, reconstruct result
		if state.Status == "committed" || state.Status == "aborted" {
			// Transaction completed - we need to get the result from somewhere
			// Since the state file might be deleted, check if we have it cached
			p.mu.Lock()
			if p.result != nil {
				result := p.result
				p.mu.Unlock()
				return result
			}
			p.mu.Unlock()

			// If state is committed, read from transaction log to get commit number
			if state.Status == "committed" {
				// Read transaction log to find the commit number
				entries, err := p.tx.storage.ReadTxLog(nil)
				if err == nil {
					for _, entry := range entries {
						if entry.TxID == p.tx.txID {
							result := &TxResult{
								Committed: true,
								Commit:    entry.Commit,
								Error:     nil,
							}
							// Cache it for this instance
							p.mu.Lock()
							if p.result == nil {
								p.result = result
								p.committed = true
								close(p.done)
							}
							result = p.result
							p.mu.Unlock()
							return result
						}
					}
				}
			} else {
				// Aborted - create error result
				result := &TxResult{
					Committed: false,
					Commit:    0,
					Error:     fmt.Errorf("transaction %d was aborted", p.tx.txID),
				}
				p.mu.Lock()
				if p.result == nil {
					p.result = result
					p.committed = true
					close(p.done)
				}
				result = p.result
				p.mu.Unlock()
				return result
			}
		}

		// Still pending - wait a bit and check again
		// Use a small sleep to avoid busy-waiting
		// In a real implementation, we might want to use a more sophisticated
		// notification mechanism, but polling works for now
		select {
		case <-p.done:
			// This instance's done channel was closed (by setResult on this instance)
			p.mu.Lock()
			result := p.result
			p.mu.Unlock()
			return result
		case <-time.After(10 * time.Millisecond):
			// Small sleep to avoid busy-waiting, then check again
		}
	}
}

// GetResult returns the current result without waiting.
// Returns nil if the transaction hasn't completed yet.
//
// This method is non-blocking and can be used to poll for completion.
// For blocking behavior, use WaitForCompletion() instead.
func (p *TxPatcher) GetResult() *TxResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.result
}
