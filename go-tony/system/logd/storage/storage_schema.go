package storage

import (
	"sync"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// storageSchema manages schema state for Storage.
// It tracks the active schema and any pending migration state.
type storageSchema struct {
	mu sync.RWMutex

	// Active schema state. activeParsed is the same schema in the form key derivation
	// needs, cached rather than re-parsed per write: schemaForScope consults it on every
	// commit. It is set wherever active is, so the two cannot drift.
	active       *ir.Node
	activeCommit int64
	activeParsed *api.Schema

	// Pending migration state (nil if no migration in progress). There is no pending
	// index: the index holds stored deltas, which carry keyed arrays in the stored form
	// already, so it is the same under either schema (090mbrhsh12ksfr8mhn0).
	pending       *ir.Node
	pendingCommit int64
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

// GetPendingParsed returns the cached parsed pending schema.
// Returns nil if no migration is in progress.
func (ss *storageSchema) GetPendingParsed() *api.Schema {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.pendingParsed
}

// GetActiveParsed returns the active schema in the form key derivation needs, or nil if
// the store is schemaless. This is the authority for what keys an array; see
// Storage.schemaForScope.
func (ss *storageSchema) GetActiveParsed() *api.Schema {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.activeParsed
}

// SetActive sets the active schema state.
// Call with nil schema and 0 commit to reset to schemaless.
//
// The parsed form is derived here rather than passed in, so no caller can set one without
// the other -- unlike SetPending, whose caller has the parsed form in hand already.
func (ss *storageSchema) SetActive(schema *ir.Node, commit int64) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.active = schema
	ss.activeCommit = commit
	ss.activeParsed = api.ParseSchemaFromNode(schema)
}

// SetPending sets the pending migration state.
func (ss *storageSchema) SetPending(schema *ir.Node, commit int64, parsed *api.Schema) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.pending = schema
	ss.pendingCommit = commit
	ss.pendingParsed = parsed
}

// ClearPending clears all pending migration state.
func (ss *storageSchema) ClearPending() {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.pending = nil
	ss.pendingCommit = 0
	ss.pendingParsed = nil
}

// PromotePending promotes the pending schema to active and clears pending state.
func (ss *storageSchema) PromotePending(commit int64) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.active = ss.pending
	ss.activeCommit = commit
	ss.activeParsed = ss.pendingParsed
	ss.pending = nil
	ss.pendingCommit = 0
	ss.pendingParsed = nil
}
