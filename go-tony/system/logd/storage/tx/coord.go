package tx

import (
	"fmt"
	"sync"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// txCoord is a coordinator/handle for a transaction.
// It wraps txCoord.State and provides transaction operations.
// Implements txCoord.Tx interface.
type txCoord struct {
	mu            sync.RWMutex // Protects state updates
	storage       Store
	commitOps     CommitOps
	state         *State // Core transaction state
	expectedCount int    // Expected number of participants

	// Coordination for multi-participant transactions
	ready      chan struct{}  // Closed when all participants have joined
	readyOnce  sync.Once      // Ensures ready is closed only once
	commitOnce sync.Once      // Ensures only one goroutine performs commit
	patchers   []*txPatcher   // All patchers for this transaction
	result     *Result        // Shared result after commit
	resultMu   sync.RWMutex   // Protects result
}

func New(store Store, commitOps CommitOps, state *State) Tx {
	return &txCoord{
		storage:       store,
		commitOps:     commitOps,
		state:         state,
		expectedCount: cap(state.PatcherData), // Use capacity, not length
		ready:         make(chan struct{}),
	}
}

// txPatcher is a participant's handle to a transaction.
// Multiple goroutines can safely call methods concurrently on patchers for the same transaction.
// Implements Patcher interface.
type txPatcher struct {
	coord   *txCoord
	done    bool
	matched bool
	allDone chan struct{} // closed when transaction completes
	data    *PatcherData
	result  *Result
	mu      sync.Mutex // protects committed, done, result
}

// Ensure tx implements Tx
var _ Tx = (*txCoord)(nil)

// Ensure txPatcher implements Patcher
var _ Patcher = (*txPatcher)(nil)

// ID returns the transaction ID, useful for sharing with other participants.
func (co *txCoord) ID() int64 {
	co.mu.RLock()
	defer co.mu.RUnlock()
	return co.state.TxID
}

// CreatedAt returns when the transaction was created.
func (co *txCoord) CreatedAt() time.Time {
	co.mu.RLock()
	defer co.mu.RUnlock()
	return co.state.CreatedAt
}

// Timeout returns the configured timeout for this transaction.
func (co *txCoord) Timeout() time.Duration {
	co.mu.RLock()
	defer co.mu.RUnlock()
	return co.state.Timeout
}

// UpdateState atomically updates the transaction state.
func (co *txCoord) UpdateState(updateFn func(*State) error) error {
	co.mu.Lock()

	// Get fresh tx from store
	tx, err := co.storage.Get(co.state.TxID)
	if err != nil {
		co.mu.Unlock()
		return fmt.Errorf("failed to get transaction state: %w", err)
	}
	if tx == nil {
		co.mu.Unlock()
		return fmt.Errorf("transaction %d not found", co.state.TxID)
	}

	// Apply update (while holding lock)
	txCoord := tx.(*txCoord)
	if err := updateFn(txCoord.state); err != nil {
		co.mu.Unlock()
		return err
	}

	// Update local state reference
	co.state = txCoord.state

	// Unlock before calling Put (which calls ID() and needs the lock)
	co.mu.Unlock()

	// Save back to store (no lock held, so Put can call ID())
	if err := co.storage.Put(tx); err != nil {
		return fmt.Errorf("failed to save transaction state: %w", err)
	}

	return nil
}

// IsComplete returns true if all expected participants have submitted their patches.
func (co *txCoord) IsComplete() bool {
	co.mu.RLock()
	defer co.mu.RUnlock()
	return len(co.state.PatcherData) >= co.expectedCount
}

// NewPatcher creates a new patcher handle for this transaction.
// Each participant should get their own patcher.
// When all expected participants have joined, the ready channel is closed
// to signal that Commit can proceed.
func (co *txCoord) NewPatcher(p *api.Patch) (Patcher, error) {
	var res *txPatcher
	var complete bool
	err := co.UpdateState(func(st *State) error {
		if len(st.PatcherData) == cap(st.PatcherData) {
			return fmt.Errorf("%d/%d patchers already added", len(st.PatcherData), len(st.PatcherData))
		}
		pData := &PatcherData{
			API:        p,
			ReceivedAt: time.Now(),
		}
		st.PatcherData = append(st.PatcherData, pData)
		res = &txPatcher{
			coord:   co,
			allDone: make(chan struct{}),
			data:    pData,
		}
		co.patchers = append(co.patchers, res)
		complete = len(st.PatcherData) >= co.expectedCount
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Signal ready when all participants have joined
	if complete {
		co.readyOnce.Do(func() {
			close(co.ready)
		})
	}

	return res, nil
}

// Commit commits all pending diffs atomically.
//
// This method is idempotent - if called multiple times or after the transaction is already
// committed, it returns the existing result.
//
// For multi-participant transactions, Commit blocks until all participants have joined
// (by calling NewPatcher). The first patcher to reach the commit logic performs the actual
// commit, and all patchers receive the same shared result.
//
// Commit flow:
// 1. Wait for all participants to join (ready channel)
// 2. Check if already committed (idempotent)
// 3. Read transaction state
// 4. Evaluate all match conditions atomically
// 5. If any match fails → abort transaction (delete state, set error result)
// 6. If all matches pass → write create log entry, set success result
// 7. Share result with all patchers
//
// Errors are returned in Result.Error, not as a separate error return.
func (p *txPatcher) Commit() *Result {
	co := p.coord

	// Wait for all participants to join, with optional timeout
	co.mu.RLock()
	timeout := co.state.Timeout
	co.mu.RUnlock()

	if timeout > 0 {
		select {
		case <-co.ready:
			// All participants joined
		case <-time.After(timeout):
			// Timeout waiting for participants - use commitOnce to set result exactly once
			co.commitOnce.Do(func() {
				_ = co.storage.Delete(co.state.TxID)
				co.resultMu.Lock()
				co.result = &Result{
					Committed: false,
					Matched:   false,
					Error:     fmt.Errorf("transaction timeout: not all participants joined within %v", timeout),
				}
				co.resultMu.Unlock()
			})
			co.resultMu.RLock()
			result := co.result
			co.resultMu.RUnlock()
			return result
		}
	} else {
		// No timeout configured - wait indefinitely
		<-co.ready
	}

	// Use commitOnce to ensure only one goroutine performs the commit.
	// All other goroutines will skip the Do() and read the shared result.
	co.commitOnce.Do(func() {
		co.mu.RLock()
		state := co.state
		commitOps := co.commitOps
		co.mu.RUnlock()

		var result *Result

		if commitOps == nil {
			result = &Result{
				Committed: false,
				Matched:   false,
				Error:     fmt.Errorf("commit operations not available"),
			}
		} else {
			result = p.doCommit(state, commitOps)
		}

		co.resultMu.Lock()
		co.result = result
		co.resultMu.Unlock()
	})

	// All goroutines return the shared result
	co.resultMu.RLock()
	result := co.result
	co.resultMu.RUnlock()
	return result
}

// doCommit performs the actual commit logic. Called exactly once via commitOnce.
func (p *txPatcher) doCommit(state *State, commitOps CommitOps) *Result {
	co := p.coord

	currentCommit, err := commitOps.GetCurrentCommit()
	if err != nil {
		return &Result{
			Committed: false,
			Matched:   false,
			Error:     fmt.Errorf("failed to get current commit: %w", err),
		}
	}

	matched, err := evaluateMatches(state, commitOps.ReadStateAt, currentCommit)
	if err != nil {
		_ = co.storage.Delete(state.TxID)
		return &Result{
			Committed: false,
			Matched:   false,
			Error:     fmt.Errorf("match evaluation failed: %w", err),
		}
	}
	if !matched {
		_ = co.storage.Delete(state.TxID)
		return &Result{
			Committed: false,
			Matched:   false,
			Error:     nil,
		}
	}

	// Tag each patch data root for streaming processor
	TagPatchRoots(state.PatcherData)

	mergedPatch, err := MergePatches(state.PatcherData)
	if err != nil {
		_ = co.storage.Delete(state.TxID)
		return &Result{
			Committed: false,
			Matched:   true,
			Error:     fmt.Errorf("failed to merge patches: %w", err),
		}
	}

	commit, err := commitOps.NextCommit()
	if err != nil {
		_ = co.storage.Delete(state.TxID)
		return &Result{
			Committed: false,
			Matched:   true,
			Error:     fmt.Errorf("failed to allocate commit: %w", err),
		}
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	lastCommit := commit - 1
	if commit == 1 {
		lastCommit = 0
	}

	_, _, err = commitOps.WriteAndIndex(commit, state.TxID, timestamp, mergedPatch, state, lastCommit)
	if err != nil {
		_ = co.storage.Delete(state.TxID)
		return &Result{
			Committed: false,
			Matched:   true,
			Error:     fmt.Errorf("failed to write entry: %w", err),
		}
	}

	_ = co.storage.Delete(state.TxID)

	// Strip internal tags from original patch data before returning
	for _, pd := range state.PatcherData {
		StripPatchRootTag(pd.API.Patch.Data)
	}

	return &Result{
		Committed: true,
		Matched:   true,
		Commit:    commit,
		Error:     nil,
	}
}
