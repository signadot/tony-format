package server

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// The schema and scopes: the operations that change what a session is working against
// rather than what it reads or writes.

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
		s.handleSchemaGet(id, req.Get)
	case req.Set != nil:
		s.handleSchemaSet(id, req.Set)
	default:
		s.sendError(id, api.ErrCodeInvalidMessage, "schema request must specify get or set")
	}
}

// handleSchemaGet answers the schema in force now, or at the commit asked about.
func (s *Session) handleSchemaGet(id *string, req *api.SchemaGetRequest) {
	if req.At != nil {
		schema, commit := s.storage.SchemaAt(*req.At)
		s.send(api.NewSchemaResponse(id, schema, commit))
		return
	}
	schema, commit := s.storage.GetActiveSchema()
	s.send(api.NewSchemaResponse(id, schema, commit))
}

// handleSchemaSet sets the schema, as one commit, and answers it.
func (s *Session) handleSchemaSet(id *string, req *api.SchemaSetRequest) {
	// Only baseline sessions can modify schema
	if s.scopeID() != nil {
		s.sendError(id, api.ErrCodeInvalidMessage, "only baseline sessions can modify schema")
		return
	}
	commit, err := s.storage.SetSchema(req.Schema, req.Force)
	if err != nil {
		// A schema the store will not adopt is the client's to change; a store that
		// could not write it is not.
		if storage.IsSchemaRefused(err) {
			s.sendError(id, api.ErrCodeSchemaRefused, err.Error())
		} else {
			s.sendError(id, api.ErrCodeStorage, fmt.Sprintf("failed to set schema: %v", err))
		}
		return
	}
	s.send(api.NewSchemaResponse(id, req.Schema, commit))
}
