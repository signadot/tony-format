package tx

import (
	"fmt"
	"sync"
	"time"
)

// Ensure InMemoryTxStore implements tx.Store
var _ Store = (*InMemoryTxStore)(nil)

// InMemoryTxStore is an in-memory implementation of Store.
// It includes a background cleanup goroutine that removes expired transactions.
type InMemoryTxStore struct {
	mu   sync.RWMutex
	d    map[int64]Tx
	done chan struct{}
}

// NewInMemoryTxStore creates a new in-memory transaction store.
// It starts a background goroutine to clean up expired transactions.
func NewInMemoryTxStore() *InMemoryTxStore {
	s := &InMemoryTxStore{
		d:    make(map[int64]Tx),
		done: make(chan struct{}),
	}
	go s.cleanupLoop()
	return s
}

// Close stops the cleanup goroutine.
func (s *InMemoryTxStore) Close() {
	close(s.done)
}

// cleanupLoop periodically removes expired transactions.
func (s *InMemoryTxStore) cleanupLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.cleanupExpired()
		}
	}
}

// cleanupExpired removes transactions that have exceeded their timeout.
// Uses the same Timeout value that Commit() uses for inline timeouts.
func (s *InMemoryTxStore) cleanupExpired() {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	for id, tx := range s.d {
		timeout := tx.Timeout()
		if timeout > 0 && now.Sub(tx.CreatedAt()) > timeout {
			delete(s.d, id)
		}
	}
}

// Get retrieves a transaction state by ID.
func (s *InMemoryTxStore) Get(txID int64) (Tx, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.d[txID], nil
}

// Put stores or updates a transaction state.
func (s *InMemoryTxStore) Put(tx Tx) error {
	if tx == nil {
		return fmt.Errorf("cannot store nil transaction state")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d[tx.ID()] = tx
	return nil
}

// Delete removes a transaction.
func (s *InMemoryTxStore) Delete(txID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.d, txID)
	return nil
}

// List returns all transaction IDs.
func (s *InMemoryTxStore) List() ([]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]int64, 0, len(s.d))
	for id := range s.d {
		ids = append(ids, id)
	}
	return ids, nil
}
