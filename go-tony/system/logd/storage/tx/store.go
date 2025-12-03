package tx

import (
	"fmt"
	"sync"
)

// Ensure InMemoryTxStore implements tx.Store
var _ Store = (*InMemoryTxStore)(nil)

// InMemoryTxStore is an in-memory implementation of Store.
// Suitable for development/testing. Can be replaced with disk-based implementation later.
type InMemoryTxStore struct {
	mu sync.RWMutex
	d  map[int64]Tx
}

// NewInMemoryTxStore creates a new in-memory transaction store.
func NewInMemoryTxStore() *InMemoryTxStore {
	return &InMemoryTxStore{
		d: make(map[int64]Tx),
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
