package api

import (
	"github.com/signadot/tony-format/go-tony/ir"
)

// Session protocol message types for bidirectional communication.
// All messages are newline-delimited Tony documents.
//
// Sync vs Async:
//   - No ID field = synchronous (client blocks until response)
//   - With ID field = asynchronous (client can pipeline, match responses by ID)

// --- Client → Server Messages ---

// Hello is the initial handshake message from client to server.
//
//tony:schemagen=session-hello,notag
type Hello struct {
	ClientID   string  `tony:"field=clientId"`
	Scope      *string `tony:"field=scope"`      // Optional: scope for COW isolation (applies to all operations in session)
	UsePending bool    `tony:"field=usePending"` // If true, use pending schema/index (for testing migrations)
}

// HelloResponse is the server's response to a Hello message.
//
//tony:schemagen=session-hello-response,notag
type HelloResponse struct {
	ServerID     string   `tony:"field=serverId"`
	Schema       *ir.Node `tony:"field=schema"`       // Server's schema (active or pending based on UsePending)
	SchemaCommit int64    `tony:"field=schemaCommit"` // Commit where this schema was set (0 if schemaless)
	UsingPending bool     `tony:"field=usingPending"` // True if session is using pending schema
}

// MatchRequest is a request to read state at a path.
//
//tony:schemagen=session-match-request,notag
type MatchRequest struct {
	Body PathData `tony:"field=body"`
}

// PatchRequest is a request to apply a patch.
// If TxID is set, the patch joins an existing multi-participant transaction.
// If TxID is nil, a new single-participant transaction is created.
//
// If Migration is true, the patch is only indexed to the pending index during
// a schema migration. It becomes visible in baseline when migration completes.
// This is used for migration transformations (e.g., populating new fields).
// Returns an error if no migration is in progress.
//
//tony:schemagen=session-patch-request,notag
type PatchRequest struct {
	TxID      *int64  `tony:"field=txId"`      // Optional: transaction ID for multi-participant tx
	Timeout   *string `tony:"field=timeout"`   // Optional: timeout for this participant (e.g., "5s", "1m")
	Migration bool    `tony:"field=migration"` // If true, only index to pending (for migration transforms)
	PathData  `tony:"field=patch"`
}

// NewTxRequest creates a new multi-participant transaction.
// The transaction will wait for the specified number of participants
// to submit their patches before committing atomically.
//
//tony:schemagen=session-newtx-request,notag
type NewTxRequest struct {
	Participants int `tony:"field=participants"` // Number of expected participants (must be >= 1)
}

// WatchRequest is a request to watch changes at a path.
//
//tony:schemagen=session-watch-request,notag
type WatchRequest struct {
	Path       string `tony:"field=path"`
	FromCommit *int64 `tony:"field=fromCommit"` // Starting commit (nil = current)
	NoInit     bool   `tony:"field=noInit"`     // If true, skip initial state (default: send initial state)
}

// UnwatchRequest is a request to stop watching a path.
//
//tony:schemagen=session-unwatch-request,notag
type UnwatchRequest struct {
	Path string `tony:"field=path"`
}

// DeleteScopeRequest deletes a scope and all its data.
// Only available from baseline sessions (no scope in hello).
//
//tony:schemagen=session-delete-scope-request,notag
type DeleteScopeRequest struct {
	ScopeID string `tony:"field=scopeId"`
}

// SchemaGetRequest requests the current schema state.
//
//tony:schemagen=session-schema-get-request,notag
type SchemaGetRequest struct{}

// SchemaSetRequest starts a schema migration to a new schema.
// This always starts a migration - use MigrationRequest.Complete to finalize.
//
// A storage without an explicit schema uses an implicit "accept-all" schema.
// The first SchemaSetRequest migrates from accept-all to the specified schema.
//
// # Auto-ID Field Changes During Migration
//
// During migration, regular patches are dual-indexed to both the active and
// pending indexes. However, auto-ID injection (!logd-auto-id) uses only the
// ACTIVE schema. This has important implications:
//
// Adding a new auto-ID field: If the pending schema adds a new field with
// !logd-auto-id (or adds !logd-auto-id to an existing field), regular patches
// during migration will NOT auto-generate values for that field. Use a
// two-phase approach: (1) migrate to add the new field WITHOUT !logd-auto-id,
// use MigrationPatchRequest to populate existing records, complete migration;
// (2) then migrate again to add !logd-auto-id to the field.
//
// Removing an auto-ID field: If the pending schema removes !logd-auto-id from
// a field (or removes the field entirely), existing auto-generated values
// remain in the data. Regular patches during migration will continue to
// auto-generate values based on the active schema until migration completes.
//
//tony:schemagen=session-schema-set-request,notag
type SchemaSetRequest struct {
	Schema *ir.Node `tony:"field=schema"` // New schema to migrate to
}

// SchemaRequest is a request for schema operations.
// Only one of the fields should be set.
//
//tony:schemagen=session-schema-request,notag
type SchemaRequest struct {
	Get *SchemaGetRequest `tony:"field=get"` // Get current schema state
	Set *SchemaSetRequest `tony:"field=set"` // Start migration to new schema
}

// MigrationAction represents a migration lifecycle action.
// Valid values are "complete" or "abort".
//
// Note: Migration patches are sent via PatchRequest with Migration=true.
//
//tony:schema=.[complete,abort]
type MigrationAction string

const (
	MigrationComplete MigrationAction = "complete"
	MigrationAbort    MigrationAction = "abort"
)

// MarshalText implements encoding.TextMarshaler.
func (a MigrationAction) MarshalText() ([]byte, error) {
	return []byte(a), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (a *MigrationAction) UnmarshalText(text []byte) error {
	*a = MigrationAction(text)
	return nil
}

// SessionRequest is the top-level request message (union type).
// Only one of the fields should be set.
//
//tony:schemagen=session-request,notag
type SessionRequest struct {
	ID *string `tony:"field=id"` // Optional: if set, response will include this ID (async mode)

	Hello       *Hello              `tony:"field=hello"`
	Match       *MatchRequest       `tony:"field=match"`
	Patch       *PatchRequest       `tony:"field=patch"`
	NewTx       *NewTxRequest       `tony:"field=newtx"`
	Watch       *WatchRequest       `tony:"field=watch"`
	Unwatch     *UnwatchRequest     `tony:"field=unwatch"`
	DeleteScope *DeleteScopeRequest `tony:"field=deleteScope"`
	Schema      *SchemaRequest      `tony:"field=schema"`
	Migration   *MigrationAction    `tony:"field=migration"` // "complete" or "abort"
}

// --- Server → Client Messages ---

// MatchResult is the result of a match request.
//
//tony:schemagen=session-match-result,notag
type MatchResult struct {
	Commit int64    `tony:"field=commit"`
	Body   *ir.Node `tony:"field=body"`
}

// PatchResult is the result of a patch request.
//
//tony:schemagen=session-patch-result,notag
type PatchResult struct {
	Commit int64    `tony:"field=commit"`
	Data   *ir.Node `tony:"field=data"` // The patched data (with any auto-generated IDs)
}

// NewTxResult is the result of a newtx request.
//
//tony:schemagen=session-newtx-result,notag
type NewTxResult struct {
	TxID int64 `tony:"field=txId"` // Transaction ID for use in subsequent patch requests
}

// WatchResult is the result of a watch request.
//
//tony:schemagen=session-watch-result,notag
type WatchResult struct {
	Watching    string `tony:"field=watching"`    // The path being watched
	ReplayingTo *int64 `tony:"field=replayingTo"` // If replaying, the commit we'll replay up to
}

// UnwatchResult is the result of an unwatch request.
//
//tony:schemagen=session-unwatch-result,notag
type UnwatchResult struct {
	Unwatched string `tony:"field=unwatched"` // The path that was unwatched
}

// DeleteScopeResult is the result of a deleteScope request.
//
//tony:schemagen=session-delete-scope-result,notag
type DeleteScopeResult struct {
	ScopeID string `tony:"field=scopeId"` // The deleted scope ID
}

// SchemaResult is the result of a schema get/set request.
//
//tony:schemagen=session-schema-result,notag
type SchemaResult struct {
	Active        *ir.Node `tony:"field=active"`        // Current active schema (nil = schemaless)
	ActiveCommit  int64    `tony:"field=activeCommit"`  // Commit where active schema was set
	Pending       *ir.Node `tony:"field=pending"`       // Pending schema if migration in progress (nil = none)
	PendingCommit int64    `tony:"field=pendingCommit"` // Commit where pending schema was set (0 if none)
}

// MigrationResult is the result of a migration complete/abort request.
//
//tony:schemagen=session-migration-result,notag
type MigrationResult struct {
	Completed bool  `tony:"field=completed"` // true if migration was completed, false if aborted
	Commit    int64 `tony:"field=commit"`    // Commit where the operation occurred
}

// SessionResult is the result of a request (union type).
// Only one of the fields should be set.
//
//tony:schemagen=session-result,notag
type SessionResult struct {
	Hello       *HelloResponse     `tony:"field=hello"`
	Match       *MatchResult       `tony:"field=match"`
	Patch       *PatchResult       `tony:"field=patch"`
	NewTx       *NewTxResult       `tony:"field=newtx"`
	Watch       *WatchResult       `tony:"field=watch"`
	Unwatch     *UnwatchResult     `tony:"field=unwatch"`
	DeleteScope *DeleteScopeResult `tony:"field=deleteScope"`
	Schema      *SchemaResult      `tony:"field=schema"`
	Migration   *MigrationResult   `tony:"field=migration"`
}

// WatchEvent is a streaming event from a watch.
//
//tony:schemagen=watch-event,notag
type WatchEvent struct {
	Commit         int64    `tony:"field=commit"`
	Path           string   `tony:"field=path"`
	State          *ir.Node `tony:"field=state"`                   // Full state (when fullState=true for first event)
	Patch          *ir.Node `tony:"field=patch"`                   // Delta patch (for subsequent events)
	ReplayComplete bool     `tony:"field=replayComplete,omitzero"` // Marker that replay is complete
}

// SessionError is an error response.
//
//tony:schemagen=session-error,notag
type SessionError struct {
	Code    string `tony:"field=code"`
	Message string `tony:"field=message"`
}

// Error implements the error interface.
func (e *SessionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code != "" {
		return e.Code + ": " + e.Message
	}
	return e.Message
}

// SessionResponse is the top-level response message (union type).
// Only one of Result, Event, or Error should be set.
//
//tony:schemagen=session-response,notag
type SessionResponse struct {
	ID *string `tony:"field=id"` // Matches request ID for async mode

	Result *SessionResult `tony:"field=result"`
	Event  *WatchEvent    `tony:"field=event"`
	Error  *SessionError  `tony:"field=error"`
}

// --- Error codes ---

const (
	ErrCodeSessionClosed   = "session_closed"
	ErrCodeInvalidMessage  = "invalid_message"
	ErrCodeInvalidWatch    = "invalid_watch"
	ErrCodeNotWatching     = "not_watching"
	ErrCodeAlreadyWatching = "already_watching"
	ErrCodeCommitNotFound  = "commit_not_found"
	ErrCodeInvalidTx       = "invalid_tx"        // Invalid transaction parameters
	ErrCodeTxNotFound      = "tx_not_found"      // Transaction ID not found
	ErrCodeTxFull          = "tx_full"           // Transaction already has all participants
	ErrCodeTxScopeMismatch = "tx_scope_mismatch" // Participant scope doesn't match transaction scope
	ErrCodeMatchFailed     = "match_failed"      // Transaction match condition failed
	ErrCodeReplayFailed    = "replay_failed"     // Watch replay failed, data may be incomplete
	ErrCodeTimeout         = "timeout"           // Operation timed out
	ErrCodeScopeExists     = "scope_exists"      // Scope already exists
	ErrCodeScopeNotFound   = "scope_not_found"   // Scope not found

	// Schema/migration error codes
	ErrCodeMigrationInProgress   = "migration_in_progress"    // Cannot start migration when one is already in progress
	ErrCodeNoMigrationInProgress = "no_migration_in_progress" // Cannot complete/abort when no migration in progress
	ErrCodeMigrationAborted      = "migration_aborted"        // Session was using pending schema but migration was aborted
	ErrCodeNoPendingMigration    = "no_pending_migration"     // UsePending requested but no migration in progress
)

// NewSessionError creates a new SessionError.
func NewSessionError(code, message string) *SessionError {
	return &SessionError{
		Code:    code,
		Message: message,
	}
}

// --- Helper constructors ---

// NewMatchResponse creates a response for a match request.
func NewMatchResponse(id *string, commit int64, body *ir.Node) *SessionResponse {
	return &SessionResponse{
		ID: id,
		Result: &SessionResult{
			Match: &MatchResult{
				Commit: commit,
				Body:   body,
			},
		},
	}
}

// NewPatchResponse creates a response for a patch request.
func NewPatchResponse(id *string, commit int64, data *ir.Node) *SessionResponse {
	return &SessionResponse{
		ID: id,
		Result: &SessionResult{
			Patch: &PatchResult{
				Commit: commit,
				Data:   data,
			},
		},
	}
}

// NewWatchResponse creates a response for a watch request.
func NewWatchResponse(id *string, path string, replayingTo *int64) *SessionResponse {
	return &SessionResponse{
		ID: id,
		Result: &SessionResult{
			Watch: &WatchResult{
				Watching:    path,
				ReplayingTo: replayingTo,
			},
		},
	}
}

// NewUnwatchResponse creates a response for an unwatch request.
func NewUnwatchResponse(id *string, path string) *SessionResponse {
	return &SessionResponse{
		ID: id,
		Result: &SessionResult{
			Unwatch: &UnwatchResult{
				Unwatched: path,
			},
		},
	}
}

// NewDeleteScopeResponse creates a response for a deleteScope request.
func NewDeleteScopeResponse(id *string, scopeID string) *SessionResponse {
	return &SessionResponse{
		ID: id,
		Result: &SessionResult{
			DeleteScope: &DeleteScopeResult{
				ScopeID: scopeID,
			},
		},
	}
}

// NewStateEvent creates an event with full state.
func NewStateEvent(commit int64, path string, state *ir.Node) *SessionResponse {
	return &SessionResponse{
		Event: &WatchEvent{
			Commit: commit,
			Path:   path,
			State:  state,
		},
	}
}

// NewPatchEvent creates an event with a delta patch.
func NewPatchEvent(commit int64, path string, patch *ir.Node) *SessionResponse {
	return &SessionResponse{
		Event: &WatchEvent{
			Commit: commit,
			Path:   path,
			Patch:  patch,
		},
	}
}

// NewReplayCompleteEvent creates a replay complete marker event.
func NewReplayCompleteEvent() *SessionResponse {
	return &SessionResponse{
		Event: &WatchEvent{
			ReplayComplete: true,
		},
	}
}

// NewErrorResponse creates an error response.
func NewErrorResponse(id *string, code, message string) *SessionResponse {
	return &SessionResponse{
		ID: id,
		Error: &SessionError{
			Code:    code,
			Message: message,
		},
	}
}

// NewSchemaResponse creates a response for a schema get/set request.
func NewSchemaResponse(id *string, active *ir.Node, activeCommit int64, pending *ir.Node, pendingCommit int64) *SessionResponse {
	return &SessionResponse{
		ID: id,
		Result: &SessionResult{
			Schema: &SchemaResult{
				Active:        active,
				ActiveCommit:  activeCommit,
				Pending:       pending,
				PendingCommit: pendingCommit,
			},
		},
	}
}

// NewMigrationResponse creates a response for a migration complete/abort request.
func NewMigrationResponse(id *string, completed bool, commit int64) *SessionResponse {
	return &SessionResponse{
		ID: id,
		Result: &SessionResult{
			Migration: &MigrationResult{
				Completed: completed,
				Commit:    commit,
			},
		},
	}
}

