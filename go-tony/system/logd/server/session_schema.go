package server

import (
	"errors"
	"fmt"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// The schema, its migrations, and scopes: the operations that change what a session is
// working against rather than what it reads or writes.

// checkPendingValid checks if a session using pending schema is still valid.
// Returns an error message if the migration was aborted, empty string if ok.
func (s *Session) checkPendingValid() string {
	if !s.usePending.Load() {
		return ""
	}
	pendingSchema, _ := s.storage.GetPendingSchema()
	if pendingSchema == nil {
		return "migration was aborted"
	}
	return ""
}

// handleDeleteScope handles delete scope requests.
// Only baseline sessions (scope=nil) can delete scopes.
func (s *Session) handleDeleteScope(id *string, req *api.DeleteScopeRequest) {
	// Only baseline sessions can delete scopes
	if s.scopeID() != nil {
		s.sendError(id, api.ErrCodeInvalidMessage, "only baseline sessions can delete scopes")
		return
	}

	scopeID := req.ScopeID
	if scopeID == "" {
		s.sendError(id, api.ErrCodeInvalidMessage, "scopeId is required")
		return
	}

	// Delete the scope from storage
	if err := s.storage.DeleteScope(scopeID); err != nil {
		s.sendError(id, api.ErrCodeScopeNotFound, err.Error())
		return
	}

	s.send(api.NewDeleteScopeResponse(id, scopeID))
}

// handleSchema handles schema get/set requests.
// Only baseline sessions (scope=nil) can modify schema.
func (s *Session) handleSchema(id *string, req *api.SchemaRequest) {
	switch {
	case req.Get != nil:
		s.handleSchemaGet(id)
	case req.Set != nil:
		s.handleSchemaSet(id, req.Set)
	default:
		s.sendError(id, api.ErrCodeInvalidMessage, "schema request must specify get or set")
	}
}

// handleSchemaGet returns the current schema state.
func (s *Session) handleSchemaGet(id *string) {
	active, activeCommit := s.storage.GetActiveSchema()
	pending, pendingCommit := s.storage.GetPendingSchema()
	s.send(api.NewSchemaResponse(id, active, activeCommit, pending, pendingCommit))
}

// handleSchemaSet starts a schema migration.
func (s *Session) handleSchemaSet(id *string, req *api.SchemaSetRequest) {
	// Only baseline sessions can modify schema
	if s.scopeID() != nil {
		s.sendError(id, api.ErrCodeInvalidMessage, "only baseline sessions can modify schema")
		return
	}

	commit, err := s.storage.StartMigration(req.Schema)
	if err != nil {
		if errors.Is(err, storage.ErrMigrationInProgress) {
			s.sendError(id, api.ErrCodeMigrationInProgress, err.Error())
		} else {
			s.sendError(id, api.ErrCodeStorage, fmt.Sprintf("failed to start migration: %v", err))
		}
		return
	}
	active, activeCommit := s.storage.GetActiveSchema()
	s.send(api.NewSchemaResponse(id, active, activeCommit, req.Schema, commit))
}

// handleMigration handles migration complete/abort requests.
// Only baseline sessions (scope=nil) can modify schema.
func (s *Session) handleMigration(id *string, action *api.MigrationAction) {
	// Only baseline sessions can modify schema
	if s.scopeID() != nil {
		s.sendError(id, api.ErrCodeInvalidMessage, "only baseline sessions can modify schema")
		return
	}

	switch *action {
	case api.MigrationComplete:
		commit, err := s.storage.CompleteMigration()
		if err != nil {
			if errors.Is(err, storage.ErrNoMigrationInProgress) {
				s.sendError(id, api.ErrCodeNoMigrationInProgress, err.Error())
			} else {
				s.sendError(id, api.ErrCodeStorage, fmt.Sprintf("failed to complete migration: %v", err))
			}
			return
		}
		s.send(api.NewMigrationResponse(id, true, commit))

	case api.MigrationAbort:
		commit, err := s.storage.AbortMigration()
		if err != nil {
			if errors.Is(err, storage.ErrNoMigrationInProgress) {
				s.sendError(id, api.ErrCodeNoMigrationInProgress, err.Error())
			} else {
				s.sendError(id, api.ErrCodeStorage, fmt.Sprintf("failed to abort migration: %v", err))
			}
			return
		}
		s.send(api.NewMigrationResponse(id, false, commit))

	default:
		s.sendError(id, api.ErrCodeInvalidMessage, fmt.Sprintf("invalid migration action: %q", *action))
	}
}
