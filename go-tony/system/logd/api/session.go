package api

import (
	"errors"

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
// Protocol is the session protocol version the client speaks. A server which speaks a
// different one refuses the session, naming both numbers -- see ProtocolVersion for why
// that is a check rather than a convention.
//
//tony:schemagen=session-hello,notag
type Hello struct {
	ClientID   string  `tony:"field=clientId"`
	Protocol   int     `tony:"field=protocol,omitzero"` // 0 = a client from before versions existed
	Scope      *string `tony:"field=scope"`             // Optional: scope for COW isolation (applies to all operations in session)
	UsePending bool    `tony:"field=usePending"`        // If true, use pending schema/index (for testing migrations)
}

// ProtocolVersion is the session protocol this build speaks, sent in Hello and answered in
// HelloResponse. A mismatch is refused at the handshake.
//
// It exists because the protocol's safety rested on a deployment convention. A request
// field a server does not know is IGNORED, and an unread path defaults to "" -- the whole
// document for a read, the ROOT for a write (k0d4y1m6h12kr7cdgdn0). So a client one version
// ahead does not fail; it is answered, wrongly, and the wrong answer looks like success.
// "logd, docd and libctl deploy together" makes that safe and nothing enforced it: a
// mismatched pair was indistinguishable from a working one until something read the root.
//
// Versions:
//
//	0  before this existed. Accepted, with a line in the log: an old client is a
//	   deployment which has not caught up, not an attack, and refusing it would take a
//	   store down for an upgrade it did not need.
//	1  the protocol as of the flattened match request ({match: {path, data, commit}}).
const ProtocolVersion = 1

// HelloResponse is the server's response to a Hello message.
//
//tony:schemagen=session-hello-response,notag
type HelloResponse struct {
	ServerID     string   `tony:"field=serverId"`
	Protocol     int      `tony:"field=protocol,omitzero"` // the version the SERVER speaks
	Schema       *ir.Node `tony:"field=schema"`            // Server's schema (active or pending based on UsePending)
	SchemaCommit int64    `tony:"field=schemaCommit"`      // Commit where this schema was set (0 if schemaless)
	UsingPending bool     `tony:"field=usingPending"`      // True if session is using pending schema
}

// MatchRequest is a request to read state at a path: the path restricts the read to
// that subdocument, and Data, when set, is a pattern the state is matched and trimmed
// against WITHIN it.
//
// PathData is embedded, as it is in PatchRequest and as path is in WatchRequest, so
// that path means the same thing in the same place in all three. It used to sit one
// level down under `body`, which read as sensible until you noticed that a RESPONSE's
// body is the answer rather than a wrapper -- the same word, two meanings, one message
// apart. The cost was not aesthetic: a client writing {match: {path: ...}}, which is
// what the siblings taught it to write, had its path silently ignored and was answered
// from the ROOT (k0d4y1m6h12kr7cdgdn0).
//
// Commit, when set, reads historical state at that commit instead of the current
// commit — a point-in-time read. It must be in range [0, current]; logd rejects
// an out-of-range commit with ErrCodeCommitNotFound. Across docd it is the same
// commit for every source, because there is one sequence: a mount commits through the
// backing logd under a tx id docd allocates, all-or-nothing, so a composed read at a
// commit is a consistent snapshot of the whole document.
//
//tony:schemagen=session-match-request,notag
type MatchRequest struct {
	Commit   *int64 `tony:"field=commit"` // Optional: read historical state at this commit (nil = current)
	PathData `tony:"field=match"`
}

// PatchRequest is a request to apply a patch.
// If TxID is set, the patch joins an existing multi-participant transaction.
// If TxID is nil, a new single-participant transaction is created.
//
//tony:schemagen=session-patch-request,notag
type PatchRequest struct {
	TxID     *int64    `tony:"field=txId"`    // Optional: transaction ID for multi-participant tx
	Timeout  *string   `tony:"field=timeout"` // Optional: timeout for this participant (e.g., "5s", "1m")
	Match    *PathData `tony:"field=match"`   // Optional: compare-and-swap precondition — the patch commits only if the current state at Match.Path matches Match.Data
	PathData `tony:"field=patch"`
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
// FromCommit is where the watch starts. Three cases:
//
//   - nil: start at the store's current commit. No history.
//   - >= 0: an ABSOLUTE commit. The watch replays the exact delta history from it
//     before streaming live, which is how a client that knows the last commit it saw
//     resumes with no gap. Below the retained history it is refused with
//     ErrCodeReplayCompacted, because a client naming a commit is claiming to know
//     where it was and deserves to be told that history is gone.
//   - < 0: RELATIVE. -N means "the last N commits", resolved against the store's
//     watermark at the moment the watch is established: start = watermark - N, and
//     never below the retained history or zero. A relative request is a request for
//     what there is, so it is CLAMPED rather than refused -- a client asking for the
//     last thousand commits of a store that only retains four hundred wants the four
//     hundred, and does not know the floor to ask for it by number.
//
// WatchResult.ReplayingFrom says what a relative offset resolved to.
//
//tony:schemagen=session-watch-request,notag
type WatchRequest struct {
	Path       string `tony:"field=path"`
	FromCommit *int64 `tony:"field=fromCommit"` // nil = current; >= 0 absolute; < 0 relative to the watermark
	NoInit     bool   `tony:"field=noInit"`     // If true, skip initial state (default: send initial state)

	// WaitIfAbsent asks for a watch on a path that holds nothing YET.
	//
	// By default a watch on such a path is refused with not_found, for the same reason a
	// read of it is: a read answers null where a null was written, and a watch that
	// delivers null for a path nobody has written to says the same thing twice
	// (bymhrqz7h12ksas3jhn0). A client that meant to wait could not be told apart from
	// one that meant to read something and got the path wrong.
	//
	// Waiting is a real thing to want -- a controller watching for a subtree its peer has
	// not created yet -- so it is asked for rather than assumed. With it set, the watch
	// is established, delivers null, and reports the value when it arrives.
	WaitIfAbsent bool `tony:"field=waitIfAbsent"`
}

// UnwatchRequest is a request to stop watching a path.
//
// WatchID optionally targets a specific watch to cancel. logd allows only one
// watch per path per session, so it identifies watches by path and ignores this
// field. docd, however, multiplexes many client sessions onto one controller
// connection, so several watches on the same path can coexist there; docd sets
// WatchID to the id of the watch request it is cancelling so the controller
// cancels exactly that one.
//
//tony:schemagen=session-unwatch-request,notag
type UnwatchRequest struct {
	Path    string  `tony:"field=path"`
	WatchID *string `tony:"field=watchId"` // optional: target a specific watch (docd controller hop)
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
// populate the new fields with ordinary patches, complete migration;
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

	// Scope names the COW scope an operation belongs to. logd itself takes scope
	// from the connection (Hello) and ignores this field; it is set by docd when it
	// routes an op to a controller, because docd multiplexes many client sessions
	// (each with its own scope) onto one controller connection, so per-connection
	// scope cannot distinguish them. A scope-aware controller honors it.
	Scope *string `tony:"field=scope,omitzero"`

	Hello       *Hello              `tony:"field=hello"`
	Match       *MatchRequest       `tony:"field=match"`
	Patch       *PatchRequest       `tony:"field=patch"`
	NewTx       *NewTxRequest       `tony:"field=newtx"`
	Watch       *WatchRequest       `tony:"field=watch"`
	Unwatch     *UnwatchRequest     `tony:"field=unwatch"`
	DeleteScope *DeleteScopeRequest `tony:"field=deleteScope"`
	Schema      *SchemaRequest      `tony:"field=schema"`
	Migration   *MigrationAction    `tony:"field=migration"` // "complete" or "abort"
	Ping        *PingRequest        `tony:"field=ping"`      // liveness probe; answered by whatever server owns the connection
}

// PingRequest is a liveness probe. The server that owns the connection answers it
// with a Pong immediately; a client sends it periodically (with a response
// deadline) to detect a wedged or half-open session and tear it down.
//
//tony:schemagen=session-ping-request,notag
type PingRequest struct{}

// PongResult is the reply to a PingRequest. It carries the store's head commit,
// because a client which wants to know "has anything happened" should not have to
// open a watch to find out: the heartbeat it already sends can say so.
//
// The number is monotonic and chases the head; it is not a promise that the client
// has seen everything below it. Zero means the answering server does not track one
// (a router in front of several stores has no single head to report).
//
//tony:schemagen=session-pong-result,notag
type PongResult struct {
	Commit int64 `tony:"field=commit,omitzero"`
	// Floor is the oldest commit whose delta history is still retained: a watch may
	// replay from it, and not from below it. It is here for the same reason Commit is
	// -- so that a client, or a router resolving a relative cursor on a client's
	// behalf, can work out where a watch may start without a read
	// (4ses3fqsh12ks8awgnn0).
	Floor int64 `tony:"field=floor,omitzero"`
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
	// ReplayingFrom is the commit the replay starts from, when the watch is
	// replaying. It is what a RELATIVE FromCommit resolved to -- a client that asked
	// for the last N commits learns which ones it is getting, and a client whose
	// request was clamped to the retained floor can see that it was.
	ReplayingFrom *int64 `tony:"field=replayingFrom,omitzero"`
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
	Pong        *PongResult        `tony:"field=pong"`
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
	Ended          bool     `tony:"field=ended,omitzero"`          // Terminal marker: the watch has ended and the client should re-establish it
	EndReason      string   `tony:"field=endReason,omitzero"`      // Why the watch ended, from the ErrCode* vocabulary (e.g. session_mounted, session_unmounted, controller_unavailable)
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

// Is matches by CODE, so a client can ask which of the ErrCode* vocabulary it got
// without knowing how the message was phrased:
//
//	errors.Is(err, &api.SessionError{Code: api.ErrCodeNotFound})
//
// The code is the stable part of a response error. The message is prose, written for
// an operator reading a log, and it changes whenever that prose improves — so a
// client that discriminates on the message is a client that breaks on a rewording.
//
// A target with no code falls back to matching the message exactly, which is only
// useful for an error a server built without one. Mirrors (*Error).Is.
func (e *SessionError) Is(target error) bool {
	t, ok := target.(*SessionError)
	if !ok || e == nil || t == nil {
		return false
	}
	if t.Code != "" {
		return e.Code == t.Code
	}
	return t.Message != "" && e.Message == t.Message
}

// ErrorCode digs the server's ErrCode* out of err, through any wrapping, and returns
// "" if no server response is in the chain. It is the accessor form of the question
// SessionError.Is answers one code at a time — useful for switching on the code, or
// for reporting it.
//
// It reaches both response error types: SessionError (the session protocol) and
// Error (the request/response API).
func ErrorCode(err error) string {
	var se *SessionError
	if errors.As(err, &se) && se != nil {
		return se.Code
	}
	var e *Error
	if errors.As(err, &e) && e != nil {
		return e.Code
	}
	return ""
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
	// ErrCodeStorage is the store failing at something the client cannot fix: a read
	// which would not read, a write which would not write. It is not the client's path,
	// its pattern or its precondition -- those have codes of their own. It was sent as a
	// bare string from eleven places and had no constant, so no client could branch on
	// it and the error table did not list it.
	// ErrCodeProtocolMismatch is a client and a server which do not speak the same
	// session protocol. Refused at hello, because every later request would be answered
	// rather than refused -- see ProtocolVersion.
	ErrCodeProtocolMismatch = "protocol_mismatch"

	// ErrCodePathConflict is a path which disagrees with the SHAPE of what is there:
	// an index into an object, a field under a string. It is neither of its neighbours
	// -- not_found says nothing is there, invalid_path says the path can never address
	// anything -- and it was answered as not_found, so a client waiting for a value to
	// appear waited for one that is already there in a form it cannot read
	// (yy0cfe9mh12kr6pwgsn0).
	//
	// errors.Is(err, ErrPathNotFound) still holds for it: there is no value AT that
	// path. The code says why.
	ErrCodePathConflict = "path_conflict"

	ErrCodeStorage = "storage_error"
	// ErrCodeMatch is a match PATTERN which could not be applied to the state at all, as
	// distinct from one which applied and did not hold (ErrCodeMatchFailed).
	ErrCodeMatch = "match_error"

	ErrCodeSessionClosed   = "session_closed"
	ErrCodeInvalidMessage  = "invalid_message"
	ErrCodeInvalidWatch    = "invalid_watch"
	ErrCodeNotWatching     = "not_watching"
	ErrCodeAlreadyWatching = "already_watching"
	ErrCodeCommitNotFound  = "commit_not_found"
	ErrCodeInvalidTx       = "invalid_tx"             // Invalid transaction parameters
	ErrCodeTxNotFound      = "tx_not_found"           // Transaction ID not found
	ErrCodeTxFull          = "tx_full"                // Transaction already has all participants
	ErrCodeTxScopeMismatch = "tx_scope_mismatch"      // Participant scope doesn't match transaction scope
	ErrCodeMatchFailed     = "match_failed"           // Transaction match condition failed
	ErrCodeReplayFailed    = "replay_failed"          // Watch replay failed, data may be incomplete
	ErrCodeReplayCompacted = "replay_compacted"       // fromCommit is older than retained delta history; re-watch without it to re-initialize
	ErrCodeSlowConsumer    = "slow_consumer"          // Watch dropped: the client did not read fast enough to keep its buffer from filling
	ErrCodeTimeout         = "timeout"                // Operation timed out
	ErrCodeScopeExists     = "scope_exists"           // Scope already exists
	ErrCodeScopeNotFound   = "scope_not_found"        // Scope not found
	ErrCodeUnsupported     = "unsupported"            // Operation not supported by the responder (e.g. a controller declining an op it does not implement)
	ErrCodeUnavailable     = "controller_unavailable" // The controller owning a mounted subtree has crashed/disconnected and not yet remounted

	// Mount-membership watch endings. A watch spanning a path whose mount set is
	// about to change is ended so it never observes the change mid-stream; it says
	// WHICH change, because the two are not the same news to a client.
	//
	// Mounted: a subtree that was answered by one source will now be answered by a
	// controller, so a re-watch composes over more sources than before and content
	// under the mount may differ. Unmounted: a source that was there has gone, and
	// what it served is either back to base or no longer served at all. A client
	// that only re-watches treats them alike; one that reconciles, or reports, or
	// decides whether the gap it just saw was expected, does not.
	ErrCodeSessionMounted   = "session_mounted"   // A mount registered under/at this watch's path; re-watch to compose over it
	ErrCodeSessionUnmounted = "session_unmounted" // A mount at/under this watch's path was removed; re-watch without it

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
	return NewWatchResponseFrom(id, path, nil, replayingTo)
}

// NewWatchResponseFrom is NewWatchResponse with the replay's starting commit, which is
// what a relative fromCommit resolved to.
func NewWatchResponseFrom(id *string, path string, replayingFrom, replayingTo *int64) *SessionResponse {
	return &SessionResponse{
		ID: id,
		Result: &SessionResult{
			Watch: &WatchResult{
				Watching:      path,
				ReplayingFrom: replayingFrom,
				ReplayingTo:   replayingTo,
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

// NewStateEvent creates an event with full state. id, when non-nil, is the
// originating watch's request id, stamped on SessionResponse.ID so a client can
// route the event to the right watch when several watches (even on the same path)
// share one connection. A nil id keeps the legacy path-routed behavior.
func NewStateEvent(id *string, commit int64, path string, state *ir.Node) *SessionResponse {
	return &SessionResponse{
		ID: id,
		Event: &WatchEvent{
			Commit: commit,
			Path:   path,
			State:  state,
		},
	}
}

// NewPatchEvent creates an event with a delta patch. See NewStateEvent for id.
func NewPatchEvent(id *string, commit int64, path string, patch *ir.Node) *SessionResponse {
	return &SessionResponse{
		ID: id,
		Event: &WatchEvent{
			Commit: commit,
			Path:   path,
			Patch:  patch,
		},
	}
}

// NewReplayCompleteEvent creates a replay complete marker event for the given
// watch path. The path lets clients route the marker to the correct watcher when
// multiplexing several watches over one connection; id (see NewStateEvent) makes
// that routing exact when the watch carries one.
func NewReplayCompleteEvent(id *string, path string) *SessionResponse {
	return &SessionResponse{
		ID: id,
		Event: &WatchEvent{
			Path:           path,
			ReplayComplete: true,
		},
	}
}

// NewEndedEvent creates the terminal event that tells a client its watch has ended, and
// why, so it re-establishes. reason is a short code (the ErrCode* vocabulary), and commit
// is the highest commit the watch accounted for — a resume point for a gapless reconnect.
// See NewStateEvent for id.
//
// A watch that has already been confirmed must end with THIS, never with an error
// response. An error response is routed by request id, and a watch's request completed
// when the watch opened, so the id no longer matches anything in flight: the client drops
// it and waits forever on a watch the server has already abandoned. Ending a request that
// is still in flight — rejecting the watch request itself — is the error response's job.
func NewEndedEvent(id *string, path, reason string, commit int64) *SessionResponse {
	return &SessionResponse{
		ID: id,
		Event: &WatchEvent{
			Path:      path,
			Commit:    commit,
			Ended:     true,
			EndReason: reason,
		},
	}
}

// NewPongResponse creates a response to a ping (liveness probe).
func NewPongResponse(id *string, commit int64) *SessionResponse {
	return NewPongResponseAt(id, commit, 0)
}

// NewPongResponseAt is NewPongResponse with the replay floor.
func NewPongResponseAt(id *string, commit, floor int64) *SessionResponse {
	return &SessionResponse{
		ID:     id,
		Result: &SessionResult{Pong: &PongResult{Commit: commit, Floor: floor}},
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
