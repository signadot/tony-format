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
//tony:schemagen=session-hello
type Hello struct {
	ClientID string `tony:"field=clientId"`
}

// HelloResponse is the server's response to a Hello message.
//
//tony:schemagen=session-hello-response
type HelloResponse struct {
	ServerID string `tony:"field=serverId"`
}

// MatchRequest is a request to read state at a path.
//
//tony:schemagen=session-match-request
type MatchRequest struct {
	Body Body `tony:"field=body"`
}

// PatchRequest is a request to apply a patch.
// If TxID is set, the patch joins an existing multi-participant transaction.
// If TxID is nil, a new single-participant transaction is created.
//
//tony:schemagen=session-patch-request
type PatchRequest struct {
	TxID    *int64  `tony:"field=txId"`    // Optional: transaction ID for multi-participant tx
	Timeout *string `tony:"field=timeout"` // Optional: timeout for this participant (e.g., "5s", "1m")
	Patch   Body    `tony:"field=patch"`
}

// NewTxRequest creates a new multi-participant transaction.
// The transaction will wait for the specified number of participants
// to submit their patches before committing atomically.
//
//tony:schemagen=session-newtx-request
type NewTxRequest struct {
	Participants int `tony:"field=participants"` // Number of expected participants (must be >= 1)
}

// WatchRequest is a request to watch changes at a path.
//
//tony:schemagen=session-watch-request
type WatchRequest struct {
	Path       string `tony:"field=path"`
	FromCommit *int64 `tony:"field=fromCommit"` // Starting commit (nil = current)
	FullState  bool   `tony:"field=fullState"`  // If true, first event is full state at fromCommit
}

// UnwatchRequest is a request to stop watching a path.
//
//tony:schemagen=session-unwatch-request
type UnwatchRequest struct {
	Path string `tony:"field=path"`
}

// SessionRequest is the top-level request message (union type).
// Only one of the fields should be set.
//
//tony:schemagen=session-request
type SessionRequest struct {
	ID *string `tony:"field=id"` // Optional: if set, response will include this ID (async mode)

	Hello   *Hello          `tony:"field=hello"`
	Match   *MatchRequest   `tony:"field=match"`
	Patch   *PatchRequest   `tony:"field=patch"`
	NewTx   *NewTxRequest   `tony:"field=newtx"`
	Watch   *WatchRequest   `tony:"field=watch"`
	Unwatch *UnwatchRequest `tony:"field=unwatch"`
}

// --- Server → Client Messages ---

// MatchResult is the result of a match request.
//
//tony:schemagen=session-match-result
type MatchResult struct {
	Commit int64    `tony:"field=commit"`
	Body   *ir.Node `tony:"field=body"`
}

// PatchResult is the result of a patch request.
//
//tony:schemagen=session-patch-result
type PatchResult struct {
	Commit int64 `tony:"field=commit"`
}

// NewTxResult is the result of a newtx request.
//
//tony:schemagen=session-newtx-result
type NewTxResult struct {
	TxID int64 `tony:"field=txId"` // Transaction ID for use in subsequent patch requests
}

// WatchResult is the result of a watch request.
//
//tony:schemagen=session-watch-result
type WatchResult struct {
	Watching    string `tony:"field=watching"`    // The path being watched
	ReplayingTo *int64 `tony:"field=replayingTo"` // If replaying, the commit we'll replay up to
}

// UnwatchResult is the result of an unwatch request.
//
//tony:schemagen=session-unwatch-result
type UnwatchResult struct {
	Unwatched string `tony:"field=unwatched"` // The path that was unwatched
}

// SessionResult is the result of a request (union type).
// Only one of the fields should be set.
//
//tony:schemagen=session-result
type SessionResult struct {
	Hello   *HelloResponse `tony:"field=hello"`
	Match   *MatchResult   `tony:"field=match"`
	Patch   *PatchResult   `tony:"field=patch"`
	NewTx   *NewTxResult   `tony:"field=newtx"`
	Watch   *WatchResult   `tony:"field=watch"`
	Unwatch *UnwatchResult `tony:"field=unwatch"`
}

// SessionEvent is a streaming event from a watch.
//
//tony:schemagen=session-event
type SessionEvent struct {
	Commit         int64    `tony:"field=commit"`
	Path           string   `tony:"field=path"`
	State          *ir.Node `tony:"field=state"`          // Full state (when fullState=true for first event)
	Patch          *ir.Node `tony:"field=patch"`          // Delta patch (for subsequent events)
	ReplayComplete bool     `tony:"field=replayComplete"` // Marker that replay is complete
}

// SessionError is an error response.
//
//tony:schemagen=session-error
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
//tony:schemagen=session-response
type SessionResponse struct {
	ID *string `tony:"field=id"` // Matches request ID for async mode

	Result *SessionResult `tony:"field=result"`
	Event  *SessionEvent  `tony:"field=event"`
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
	ErrCodeInvalidTx       = "invalid_tx"       // Invalid transaction parameters
	ErrCodeTxNotFound      = "tx_not_found"     // Transaction ID not found
	ErrCodeTxFull          = "tx_full"          // Transaction already has all participants
	ErrCodeMatchFailed     = "match_failed"     // Transaction match condition failed
	ErrCodeReplayFailed    = "replay_failed"    // Watch replay failed, data may be incomplete
	ErrCodeTimeout         = "timeout"          // Operation timed out
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
func NewPatchResponse(id *string, commit int64) *SessionResponse {
	return &SessionResponse{
		ID: id,
		Result: &SessionResult{
			Patch: &PatchResult{
				Commit: commit,
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

// NewStateEvent creates an event with full state.
func NewStateEvent(commit int64, path string, state *ir.Node) *SessionResponse {
	return &SessionResponse{
		Event: &SessionEvent{
			Commit: commit,
			Path:   path,
			State:  state,
		},
	}
}

// NewPatchEvent creates an event with a delta patch.
func NewPatchEvent(commit int64, path string, patch *ir.Node) *SessionResponse {
	return &SessionResponse{
		Event: &SessionEvent{
			Commit: commit,
			Path:   path,
			Patch:  patch,
		},
	}
}

// NewReplayCompleteEvent creates a replay complete marker event.
func NewReplayCompleteEvent() *SessionResponse {
	return &SessionResponse{
		Event: &SessionEvent{
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
