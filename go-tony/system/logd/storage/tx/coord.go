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
}

func New(store Store, commitOps CommitOps, state *State) Tx {
	return &txCoord{
		storage:       store,
		commitOps:     commitOps,
		state:         state,
		expectedCount: len(state.PatcherData),
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
func (co *txCoord) NewPatcher(p *api.Patch) (Patcher, error) {
	var res *txPatcher
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
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// Commit commits all pending diffs atomically.
//
// This method is idempotent - if called multiple times or after the transaction is already
// committed, it returns the existing result.
//
// Commit flow:
// 1. Check if already committed (idempotent)
// 2. Read transaction state
// 3. Evaluate all match conditions atomically
// 4. If any match fails → abort transaction (delete state, set error result)
// 5. If all matches pass → write create log entry, set success result
//
// Errors are returned in Result.Error, not as a separate error return.
func (p *txPatcher) Commit() *Result {
	p.mu.Lock()
	if p.done {
		result := p.result
		p.mu.Unlock()
		return result
	}
	p.mu.Unlock()

	co := p.coord
	co.mu.RLock()
	state := co.state
	commitOps := co.commitOps
	co.mu.RUnlock()

	if commitOps == nil {
		p.mu.Lock()
		p.done = true
		p.result = &Result{
			Committed: false,
			Matched:   false,
			Error:     fmt.Errorf("commit operations not available"),
		}
		result := p.result
		p.mu.Unlock()
		return result
	}

	currentCommit, err := commitOps.GetCurrentCommit()
	if err != nil {
		p.mu.Lock()
		p.done = true
		p.result = &Result{
			Committed: false,
			Matched:   false,
			Error:     fmt.Errorf("failed to get current commit: %w", err),
		}
		result := p.result
		p.mu.Unlock()
		return result
	}

	matched, err := evaluateMatches(state, commitOps.ReadStateAt, currentCommit)
	if err != nil {
		_ = co.storage.Delete(state.TxID)
		p.mu.Lock()
		p.done = true
		p.result = &Result{
			Committed: false,
			Matched:   false,
			Error:     fmt.Errorf("match evaluation failed: %w", err),
		}
		result := p.result
		p.mu.Unlock()
		return result
	}
	if !matched {
		_ = co.storage.Delete(state.TxID)
		p.mu.Lock()
		p.done = true
		p.result = &Result{
			Committed: false,
			Matched:   false,
			Error:     nil,
		}
		result := p.result
		p.mu.Unlock()
		return result
	}

	// Tag each patch data root for streaming processor
	TagPatchRoots(state.PatcherData)

	mergedPatch, err := MergePatches(state.PatcherData)
	if err != nil {
		_ = co.storage.Delete(state.TxID)
		p.mu.Lock()
		p.done = true
		p.result = &Result{
			Committed: false,
			Matched:   true,
			Error:     fmt.Errorf("failed to merge patches: %w", err),
		}
		result := p.result
		p.mu.Unlock()
		return result
	}

	commit, err := commitOps.NextCommit()
	if err != nil {
		_ = co.storage.Delete(state.TxID)
		p.mu.Lock()
		p.done = true
		p.result = &Result{
			Committed: false,
			Matched:   true,
			Error:     fmt.Errorf("failed to allocate commit: %w", err),
		}
		result := p.result
		p.mu.Unlock()
		return result
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	lastCommit := commit - 1
	if commit == 1 {
		lastCommit = 0
	}

	_, _, err = commitOps.WriteAndIndex(commit, state.TxID, timestamp, mergedPatch, state, lastCommit)
	if err != nil {
		_ = co.storage.Delete(state.TxID)
		p.mu.Lock()
		p.done = true
		p.result = &Result{
			Committed: false,
			Matched:   true,
			Error:     fmt.Errorf("failed to write entry: %w", err),
		}
		result := p.result
		p.mu.Unlock()
		return result
	}

	_ = co.storage.Delete(state.TxID)

	// Strip internal tags from original patch data before returning
	for _, pd := range state.PatcherData {
		StripPatchRootTag(pd.API.Patch.Data)
	}

	p.mu.Lock()
	p.done = true
	p.result = &Result{
		Committed: true,
		Matched:   true,
		Commit:    commit,
		Error:     nil,
	}
	result := p.result
	p.mu.Unlock()
	return result
}
