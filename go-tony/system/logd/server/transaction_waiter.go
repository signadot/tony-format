package server

import (
	"net/http"
	"sync"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// pendingWrite represents a write waiting for transaction completion.
type pendingWrite struct {
	w         http.ResponseWriter
	r         *http.Request
	body      *api.Body
	pathStr   string
	patch     *ir.Node
	timestamp string
	txSeq     int64
}

// transactionResult holds the result of a completed transaction.
type transactionResult struct {
	committed   bool
	commitCount int64
	err         error
}

// transactionWaiter tracks pending writes for a transaction.
type transactionWaiter struct {
	mu            sync.Mutex
	stateUpdateMu sync.Mutex // Protects transaction state updates
	refCount      int        // Number of active HTTP handlers using this waiter
	pending       []pendingWrite
	done          chan struct{} // closed when transaction completes or aborts
	result        *transactionResult
}

// NewTransactionWaiter creates a new transaction waiter.
func NewTransactionWaiter() *transactionWaiter {
	return &transactionWaiter{
		pending: make([]pendingWrite, 0),
		done:    make(chan struct{}),
	}
}

// RegisterWrite registers a write with the waiter.
// Returns an error if the transaction is already completed or aborted.
func (tw *transactionWaiter) RegisterWrite(write pendingWrite) error {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	// Check if transaction is already completed/aborted
	if tw.result != nil {
		if tw.result.err != nil {
			return tw.result.err
		}
		return ErrTransactionCompleted
	}

	tw.pending = append(tw.pending, write)
	return nil
}

// UpdateState atomically updates the transaction state and returns whether this is the last participant.
func (tw *transactionWaiter) UpdateState(transactionID string, stg *storage.Storage, updateFn func(*storage.TransactionState)) (isLastParticipant bool, err error) {
	tw.stateUpdateMu.Lock()
	defer tw.stateUpdateMu.Unlock()

	var lastParticipant bool
	err = stg.UpdateTransactionState(transactionID, func(currentState *storage.TransactionState) {
		updateFn(currentState)
		lastParticipant = currentState.ParticipantsReceived >= currentState.ParticipantCount
	})

	return lastParticipant, err
}

// SetResult sets the transaction result and notifies all waiting writes.
func (tw *transactionWaiter) SetResult(result *transactionResult) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.result == nil {
		tw.result = result
		close(tw.done)
	}
}

// WaitForCompletion waits for the transaction to complete or abort and returns the result.
func (tw *transactionWaiter) WaitForCompletion() *transactionResult {
	<-tw.done

	tw.mu.Lock()
	defer tw.mu.Unlock()
	return tw.result
}

// GetResult returns the current result without waiting.
// Returns nil if the transaction hasn't completed yet.
func (tw *transactionWaiter) GetResult() *transactionResult {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	return tw.result
}
