package storage

import (
	"sync"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

// storageSchema manages schema state for Storage.
// It tracks the active schema and any pending migration state.
type storageSchema struct {
	mu sync.RWMutex

	// Active schema state
	active       *ir.Node
	activeCommit int64

	// Pending migration state (nil if no migration in progress)
	pending       *ir.Node
	pendingCommit int64
	pendingIndex  *index.Index
	pendingParsed *api.Schema
}

func newStorageSchema() *storageSchema {
	return &storageSchema{}
}

// GetActive returns the current active schema and the commit where it was set.
// Returns nil schema and 0 commit if schemaless.
func (ss *storageSchema) GetActive() (*ir.Node, int64) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.active, ss.activeCommit
}

// GetPending returns the pending schema and commit if a migration is in progress.
// Returns nil, 0 if no migration is in progress.
func (ss *storageSchema) GetPending() (*ir.Node, int64) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.pending, ss.pendingCommit
}

// HasPending returns true if a schema migration is in progress.
func (ss *storageSchema) HasPending() bool {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.pending != nil
}

// GetPendingIndex returns the pending index being built during migration.
// Returns nil if no migration is in progress.
func (ss *storageSchema) GetPendingIndex() *index.Index {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.pendingIndex
}

// GetPendingParsed returns the cached parsed pending schema.
// Returns nil if no migration is in progress.
func (ss *storageSchema) GetPendingParsed() *api.Schema {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.pendingParsed
}

// SetActive sets the active schema state.
// Call with nil schema and 0 commit to reset to schemaless.
func (ss *storageSchema) SetActive(schema *ir.Node, commit int64) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.active = schema
	ss.activeCommit = commit
}

// SetPending sets the pending migration state.
func (ss *storageSchema) SetPending(schema *ir.Node, commit int64, idx *index.Index, parsed *api.Schema) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.pending = schema
	ss.pendingCommit = commit
	ss.pendingIndex = idx
	ss.pendingParsed = parsed
}

// ClearPending clears all pending migration state.
func (ss *storageSchema) ClearPending() {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.pending = nil
	ss.pendingCommit = 0
	ss.pendingIndex = nil
	ss.pendingParsed = nil
}

// PromotePending promotes the pending schema to active and clears pending state.
// Returns the new index that should replace the active index.
func (ss *storageSchema) PromotePending(commit int64) *index.Index {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.active = ss.pending
	ss.activeCommit = commit
	newIndex := ss.pendingIndex
	ss.pending = nil
	ss.pendingCommit = 0
	ss.pendingIndex = nil
	ss.pendingParsed = nil
	return newIndex
}
