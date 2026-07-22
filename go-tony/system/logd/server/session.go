package server

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// Session represents a bidirectional session with a client.
// It handles parsing requests, dispatching to handlers, and sending responses/events.
type Session struct {
	ID      string
	conn    io.ReadWriteCloser
	storage *storage.Storage
	hub     *WatchHub
	log     *slog.Logger

	// Server schema (returned in hello response)
	schema *ir.Node

	// Scope for COW isolation (set in hello, applies to all operations)
	scope *string

	// If true, session uses pending schema/index (for testing migrations)
	usePending bool

	// Watch state
	watchMu sync.RWMutex
	watches map[string]*Watcher // path -> active watcher

	// Communication channels
	outgoing chan *api.SessionResponse // responses and events to send
	done     chan struct{}             // signals session shutdown

	// Shutdown coordination
	closeOnce    sync.Once
	closeOutOnce sync.Once

	// For tracking commits since snapshot (shared with server)
	onCommit func()
}

// SessionConfig contains configuration for creating a session.
type SessionConfig struct {
	Storage        *storage.Storage
	Hub            *WatchHub
	Log            *slog.Logger
	OnCommit       func()   // called after successful commits (for snapshot tracking)
	OutgoingBuffer int      // buffer size for outgoing channel (default 100)
	Schema         *ir.Node // Server's schema (returned in hello response)
}

// NewSession creates a new session for the given connection.
func NewSession(id string, conn io.ReadWriteCloser, cfg *SessionConfig) *Session {
	bufSize := cfg.OutgoingBuffer
	if bufSize <= 0 {
		bufSize = 100
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &Session{
		ID:       id,
		conn:     conn,
		storage:  cfg.Storage,
		hub:      cfg.Hub,
		log:      log.With("session", id),
		schema:   cfg.Schema,
		watches:  make(map[string]*Watcher),
		outgoing: make(chan *api.SessionResponse, bufSize),
		done:     make(chan struct{}),
		onCommit: cfg.OnCommit,
	}
}

// Run starts the session and blocks until it completes.
// It spawns reader and writer goroutines and waits for completion.
func (s *Session) Run() error {
	var wg sync.WaitGroup

	// Goroutine to close connection when done is signaled.
	// This unblocks the reader if it's stuck in a blocking read.
	wg.Go(func() {
		<-s.done
		s.conn.Close()
	})

	// Writer goroutine
	wg.Go(func() {
		s.writer()
	})

	// Start watch event forwarders for any existing watches
	// (none at start, but the pattern is established)

	// Reader runs in the main goroutine
	err := s.reader()

	// Signal shutdown (safe to call multiple times)
	s.closeOnce.Do(func() {
		close(s.done)
	})

	// Clean up watches
	s.cleanupWatches()

	// Close outgoing to stop writer (safe to call multiple times)
	s.closeOutOnce.Do(func() {
		close(s.outgoing)
	})

	// Wait for writer to finish
	wg.Wait()

	return err
}

// Close signals the session to shut down.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
	})
	return s.conn.Close()
}

// reader reads and processes incoming messages using stream.Decoder.
// It exits when the connection is closed (either by client disconnect or session shutdown).
func (s *Session) reader() error {
	decoder, err := stream.NewDecoder(s.conn, stream.WithBrackets())
	if err != nil {
		return fmt.Errorf("failed to create decoder: %w", err)
	}

	for {
		// Read a complete document (events until depth returns to 0).
		// This blocks until data arrives or connection is closed.
		// The connection closer goroutine in Run() ensures we unblock on shutdown.
		node, err := s.readDocument(decoder)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			// Check if this is a "use of closed connection" error from shutdown
			select {
			case <-s.done:
				return nil // Clean shutdown
			default:
			}
			return fmt.Errorf("read error: %w", err)
		}

		if node == nil {
			continue
		}

		// Parse request from node
		var req api.SessionRequest
		if err := req.FromTonyIR(node); err != nil {
			s.sendError(nil, api.ErrCodeInvalidMessage, fmt.Sprintf("failed to parse request: %v", err))
			continue
		}

		// Dispatch request
		s.dispatch(&req)
	}
}

// readDocument reads events until we have a complete document (depth returns to 0).
func (s *Session) readDocument(decoder *stream.Decoder) (*ir.Node, error) {
	var events []stream.Event
	started := false

	for {
		event, err := decoder.ReadEvent()
		if err != nil {
			if err == io.EOF {
				// EOF while reading - convert any accumulated events
				if len(events) > 0 {
					return stream.EventsToNode(events)
				}
				return nil, io.EOF
			}
			return nil, err
		}

		events = append(events, *event)
		started = true

		// Check if document is complete (depth back to 0)
		if started && decoder.Depth() == 0 {
			return stream.EventsToNode(events)
		}
	}
}

// writer sends outgoing responses and events.
func (s *Session) writer() {
	for resp := range s.outgoing {
		// Use wire format to match client's WithBrackets() decoder
		data, err := resp.ToTony(gomap.EncodeWire(true))
		if err != nil {
			s.log.Error("failed to encode response", "error", err)
			continue
		}

		// Write with newline delimiter
		if _, err := s.conn.Write(append(data, '\n')); err != nil {
			s.log.Error("failed to write response", "error", err)
			return
		}
	}
}

// dispatch routes a request to the appropriate handler.
func (s *Session) dispatch(req *api.SessionRequest) {
	switch {
	case req.Hello != nil:
		s.handleHello(req.ID, req.Hello)
	case req.Match != nil:
		s.handleMatch(req.ID, req.Match)
	case req.Patch != nil:
		s.handlePatch(req.ID, req.Patch)
	case req.NewTx != nil:
		s.handleNewTx(req.ID, req.NewTx)
	case req.Watch != nil:
		s.handleWatch(req.ID, req.Watch)
	case req.Unwatch != nil:
		s.handleUnwatch(req.ID, req.Unwatch)
	case req.DeleteScope != nil:
		s.handleDeleteScope(req.ID, req.DeleteScope)
	case req.Schema != nil:
		s.handleSchema(req.ID, req.Schema)
	case req.Migration != nil:
		s.handleMigration(req.ID, req.Migration)
	default:
		s.sendError(req.ID, api.ErrCodeInvalidMessage, "no operation specified")
	}
}

// handleHello handles hello handshake.
func (s *Session) handleHello(id *string, req *api.Hello) {
	// Store scope for this session (applies to all operations)
	s.scope = req.Scope
	s.log.Debug("hello", "clientId", req.ClientID, "scope", req.Scope, "usePending", req.UsePending)

	var schema *ir.Node
	var schemaCommit int64
	var usingPending bool

	if req.UsePending {
		// Client wants to use pending schema for testing migration
		pendingSchema, pendingCommit := s.storage.GetPendingSchema()
		if pendingSchema == nil {
			s.sendError(id, api.ErrCodeNoPendingMigration, "no migration in progress")
			return
		}
		s.usePending = true
		usingPending = true
		schema = pendingSchema
		schemaCommit = pendingCommit
	} else {
		// Use active schema (default)
		schema, schemaCommit = s.storage.GetActiveSchema()
		if schema == nil {
			schema = s.schema // Fallback to config schema (schemaCommit stays 0)
		}
	}

	s.send(&api.SessionResponse{
		ID: id,
		Result: &api.SessionResult{
			Hello: &api.HelloResponse{
				ServerID:     s.ID,
				Schema:       schema,
				SchemaCommit: schemaCommit,
				UsingPending: usingPending,
			},
		},
	})
}

// checkPendingValid checks if a session using pending schema is still valid.
// Returns an error message if the migration was aborted, empty string if ok.
func (s *Session) checkPendingValid() string {
	if !s.usePending {
		return ""
	}
	pendingSchema, _ := s.storage.GetPendingSchema()
	if pendingSchema == nil {
		return "migration was aborted"
	}
	return ""
}

// handleMatch handles match (read) requests.
func (s *Session) handleMatch(id *string, req *api.MatchRequest) {
	// Check if session using pending is still valid
	if errMsg := s.checkPendingValid(); errMsg != "" {
		s.sendError(id, api.ErrCodeMigrationAborted, errMsg)
		return
	}

	path := req.Body.Path

	// Validate path
	if err := validateDataPath(path); err != nil {
		s.sendError(id, api.ErrCodeInvalidPath, err.Error())
		return
	}

	// Resolve the commit to read at: an explicit historical commit if the request
	// carries one, otherwise the current commit. A historical commit must fall in
	// [0, current]; a commit past current would silently read as current and a
	// negative one as empty, so reject both rather than return misleading state.
	current, err := s.storage.GetCurrentCommit()
	if err != nil {
		s.sendError(id, "storage_error", fmt.Sprintf("failed to get current commit: %v", err))
		return
	}
	commit := current
	if req.Commit != nil {
		commit = *req.Commit
		if commit < 0 || commit > current {
			s.sendError(id, api.ErrCodeCommitNotFound,
				fmt.Sprintf("commit %d out of range [0, %d]", commit, current))
			return
		}
	}

	// Read state (with session scope filtering)
	doc, err := s.storage.ReadStateAt(path, commit, s.scope)
	if err != nil {
		s.sendError(id, "storage_error", fmt.Sprintf("failed to read state: %v", err))
		return
	}

	// Extract value at path
	state, err := extractPathValue(doc, path)
	if err != nil {
		if errors.Is(err, ErrPathNotFound) {
			s.sendError(id, api.ErrCodeNotFound, err.Error())
			return
		}
		s.sendError(id, "storage_error", fmt.Sprintf("failed to extract path value: %v", err))
		return
	}

	// Apply match filter if provided
	if req.Body.Data != nil && req.Body.Data.Type != ir.NullType {
		filteredState, err := filterState(state, req.Body.Data)
		if err != nil {
			s.sendError(id, "match_error", fmt.Sprintf("failed to apply match filter: %v", err))
			return
		}
		state = filteredState
	}

	s.send(api.NewMatchResponse(id, commit, state))
}

// handlePatch handles patch (write) requests.
// If TxID is provided, the patch joins an existing multi-participant transaction.
// If TxID is nil, a new single-participant transaction is created.
// If Migration is true, the patch is only indexed to pending (for migration transforms).
func (s *Session) handlePatch(id *string, req *api.PatchRequest) {
	path := req.Path

	// Check if session using pending is still valid
	if errMsg := s.checkPendingValid(); errMsg != "" {
		s.sendError(id, api.ErrCodeMigrationAborted, errMsg)
		return
	}

	// Validate path
	if err := validateDataPath(path); err != nil {
		s.sendError(id, api.ErrCodeInvalidPath, err.Error())
		return
	}

	// Validate patch data
	if req.Data == nil {
		s.sendError(id, api.ErrCodeInvalidDiff, "patch data is required")
		return
	}

	// Handle migration patches (only indexed to pending)
	if req.Migration {
		// Only baseline sessions can do migration patches
		if s.scope != nil {
			s.sendError(id, api.ErrCodeInvalidMessage, "only baseline sessions can apply migration patches")
			return
		}
		// Cannot combine Migration with TxID
		if req.TxID != nil {
			s.sendError(id, api.ErrCodeInvalidTx, "migration patches cannot use transactions")
			return
		}

		commit, data, err := s.storage.MigrationPatch(path, req.Data)
		if err != nil {
			if errors.Is(err, storage.ErrNoMigrationInProgress) {
				s.sendError(id, api.ErrCodeNoMigrationInProgress, err.Error())
			} else {
				s.sendError(id, "storage_error", fmt.Sprintf("failed to apply migration patch: %v", err))
			}
			return
		}
		s.send(api.NewPatchResponse(id, commit, data))
		return
	}

	// Parse timeout if provided
	var timeout time.Duration
	if req.Timeout != nil {
		var err error
		timeout, err = time.ParseDuration(*req.Timeout)
		if err != nil {
			s.sendError(id, api.ErrCodeInvalidTx, fmt.Sprintf("invalid timeout %q: %v", *req.Timeout, err))
			return
		}
	}

	var txn tx.Tx
	var err error

	if req.TxID != nil {
		// Join existing transaction
		txn, err = s.storage.GetTx(*req.TxID)
		if err != nil {
			s.sendError(id, api.ErrCodeTxNotFound, fmt.Sprintf("transaction %d not found: %v", *req.TxID, err))
			return
		}
		// Validate scope matches - all participants must have the same scope
		if !scopesEqual(s.scope, txn.Scope()) {
			s.sendError(id, api.ErrCodeTxScopeMismatch, fmt.Sprintf("session scope %q doesn't match transaction scope %q", scopeStr(s.scope), scopeStr(txn.Scope())))
			return
		}
		s.log.Debug("joining transaction", "txId", *req.TxID)
	} else {
		// Create single-participant transaction with session scope
		txn, err = s.storage.NewTx(1, s.scope)
		if err != nil {
			s.sendError(id, "storage_error", fmt.Sprintf("failed to create transaction: %v", err))
			return
		}
	}

	// Create patcher and commit. Match, if set, is a compare-and-swap
	// precondition evaluated atomically at commit time.
	patcher, err := txn.NewPatcher(&api.Patch{
		Match:    req.Match,
		PathData: req.PathData,
	})
	if err != nil {
		if req.TxID != nil {
			s.sendError(id, api.ErrCodeTxFull, fmt.Sprintf("failed to join transaction: %v", err))
		} else {
			s.sendError(id, "storage_error", fmt.Sprintf("failed to create patcher: %v", err))
		}
		return
	}

	// Commit with optional per-participant timeout
	var result *tx.Result
	if timeout > 0 {
		resultCh := make(chan *tx.Result, 1)
		go func() {
			resultCh <- patcher.Commit()
		}()
		select {
		case result = <-resultCh:
			// Commit completed
		case <-time.After(timeout):
			s.sendError(id, api.ErrCodeTimeout, fmt.Sprintf("patch timed out after %v", timeout))
			return
		}
	} else {
		// No timeout - block until commit completes
		result = patcher.Commit()
	}

	if result.Error != nil {
		s.sendError(id, "storage_error", fmt.Sprintf("failed to commit: %v", result.Error))
		return
	}
	if !result.Matched {
		s.sendError(id, api.ErrCodeMatchFailed, "transaction match condition failed")
		return
	}

	// Notify server for snapshot tracking
	if s.onCommit != nil {
		s.onCommit()
	}

	// Strip internal tags before sending to client
	tx.StripPatchRootTagRecursive(result.Data)
	s.send(api.NewPatchResponse(id, result.Commit, result.Data))
}

// handleNewTx handles newtx requests to create multi-participant transactions.
func (s *Session) handleNewTx(id *string, req *api.NewTxRequest) {
	// Check if session using pending is still valid
	if errMsg := s.checkPendingValid(); errMsg != "" {
		s.sendError(id, api.ErrCodeMigrationAborted, errMsg)
		return
	}

	if req.Participants < 1 {
		s.sendError(id, api.ErrCodeInvalidTx, "participants must be at least 1")
		return
	}

	tx, err := s.storage.NewTx(req.Participants, s.scope)
	if err != nil {
		s.sendError(id, "storage_error", fmt.Sprintf("failed to create transaction: %v", err))
		return
	}

	s.log.Debug("created transaction", "txId", tx.ID(), "participants", req.Participants)
	s.send(&api.SessionResponse{
		ID: id,
		Result: &api.SessionResult{
			NewTx: &api.NewTxResult{
				TxID: tx.ID(),
			},
		},
	})
}

// handleWatch handles watch requests.
func (s *Session) handleWatch(id *string, req *api.WatchRequest) {
	path := req.Path

	// Check if session using pending is still valid
	if errMsg := s.checkPendingValid(); errMsg != "" {
		s.sendError(id, api.ErrCodeMigrationAborted, errMsg)
		return
	}

	// Validate path
	if path != "" {
		if err := validateDataPath(path); err != nil {
			s.sendError(id, api.ErrCodeInvalidPath, err.Error())
			return
		}
	}

	// Check if already watching
	s.watchMu.RLock()
	_, exists := s.watches[path]
	s.watchMu.RUnlock()

	if exists {
		s.sendError(id, api.ErrCodeAlreadyWatching, fmt.Sprintf("already watching %q", path))
		return
	}

	// IMPORTANT: Register with hub FIRST to avoid race condition.
	// Events that arrive between Watch and GetCurrentCommit will be queued.
	// After replay, we skip any queued events with commit <= currentCommit.
	watcher := NewWatcher(path, s.scope, req.FromCommit, 100)
	s.hub.Watch(watcher)

	// Now get current commit - this is our replay target
	currentCommit, err := s.storage.GetCurrentCommit()
	if err != nil {
		s.hub.Unwatch(watcher)
		s.sendError(id, "storage_error", fmt.Sprintf("failed to get current commit: %v", err))
		return
	}

	// Store watcher
	s.watchMu.Lock()
	s.watches[path] = watcher
	s.watchMu.Unlock()

	// Determine replay range
	var replayingTo *int64
	if req.FromCommit != nil && *req.FromCommit < currentCommit {
		replayingTo = &currentCommit
	}

	// Send watch confirmation
	s.send(api.NewWatchResponse(id, path, replayingTo))

	// Start event forwarder goroutine
	go s.forwardEvents(watcher, req.FromCommit, req.NoInit, currentCommit)
}

// forwardEvents forwards events from a watcher to the session's outgoing channel.
// It handles initial state and replay, then forwards live events with deduplication.
//
// Race prevention: We registered with the hub BEFORE getting currentCommit.
// This means events that arrive between Watch and GetCurrentCommit are queued.
// After replay completes, we skip any queued events with commit <= currentCommit
// since they were already replayed.
//
// Error handling: If replay fails, an error event is sent and the watch is terminated.
// The client should re-establish the watch, possibly from a different commit.
func (s *Session) forwardEvents(watcher *Watcher, fromCommit *int64, noInit bool, currentCommit int64) {
	path := watcher.Path
	scoped := s.scope != nil

	// Track the highest commit we've replayed (for deduplication)
	lastReplayedCommit := int64(0)

	// Determine the starting commit for initial state
	startCommit := currentCommit
	if fromCommit != nil {
		startCommit = *fromCommit
		lastReplayedCommit = currentCommit
	}
	// Send initial state unless noInit is set
	if !noInit {
		var state *ir.Node
		if startCommit == 0 {
			// Empty store - state is null
			state = ir.Null()
		} else {
			var err error
			state, err = s.storage.ReadStateAt(path, startCommit, s.scope)
			if err != nil {
				s.log.Error("failed to read state for init", "path", path, "commit", startCommit, "error", err)
				s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read state at commit %d: %v", startCommit, err))
				return
			}
			// Extract value at path if needed
			if path != "" {
				state, err = extractPathValue(state, path)
				if err != nil {
					s.log.Error("failed to extract path value for init", "path", path, "error", err)
					state = ir.Null()
				}
			}
		}
		s.send(api.NewStateEvent(startCommit, path, state))
	}

	// prevDoc tracks the last-emitted scoped state (root-rooted) so a scoped watcher
	// can emit deltas by recompute-and-diff. A scoped watcher must reproduce the scoped
	// read at each commit; a raw committed delta (baseline or scope) does not, because
	// scope writes shadow baseline and !key merges are identity-based. Recompute+diff
	// also naturally suppresses baseline writes to leaves the scope has overridden (the
	// scoped view is unchanged -> empty diff). See issue eagjggjdh12ksg00bsn0.
	//
	// prevDoc is seeded at startCommit only for a fromCommit replay (the replay below
	// diffs forward from it). For a no-replay watcher it is seeded LAZILY, at
	// (firstEvent.commit - 1) when the first event arrives — never at startCommit —
	// because a write can race into [hub-register, GetCurrentCommit] and be queued with
	// commit <= startCommit; seeding at startCommit would fold that write into the
	// baseline and drop its delta (the scoped-watch drop-one-event regression). Lazy
	// seeding makes every queued or live event a correct forward diff.
	var prevDoc *ir.Node
	prevSeeded := false
	if scoped && fromCommit != nil {
		var err error
		prevDoc, err = s.scopedDocAt(path, startCommit)
		if err != nil {
			s.log.Error("failed to read scoped watch base", "path", path, "commit", startCommit, "error", err)
			s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read scoped state at commit %d: %v", startCommit, err))
			return
		}
		prevSeeded = true
	}

	// Handle replay if fromCommit is specified
	if fromCommit != nil {
		// Send historical patches from startCommit+1 to currentCommit
		if startCommit < currentCommit {
			patches, err := s.storage.ReadPatchesInRange(path, startCommit+1, currentCommit, s.scope)
			if err != nil {
				s.log.Error("failed to read patches for replay", "path", path, "from", startCommit+1, "to", currentCommit, "error", err)
				s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read patches from commit %d to %d: %v", startCommit+1, currentCommit, err))
				return
			}
			for _, patch := range patches {
				if scoped {
					newPrev, err := s.emitScopedDelta(path, patch.Commit, prevDoc)
					if err != nil {
						s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read scoped state at commit %d: %v", patch.Commit, err))
						return
					}
					prevDoc = newPrev
					continue
				}
				tx.StripPatchRootTagRecursive(patch.Patch)
				s.send(api.NewPatchEvent(patch.Commit, path, patch.Patch))
			}
		}

		s.send(api.NewReplayCompleteEvent(path))
	}

	// Forward live events, skipping any already replayed
	for {
		select {
		case <-s.done:
			return
		case <-watcher.Failed:
			// Watch failed (slow consumer)
			s.log.Warn("watch failed (slow consumer)", "path", path)
			s.failWatch(watcher, api.ErrCodeSessionClosed, fmt.Sprintf("watch on %q failed: slow consumer", path))
			return
		case notification, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Skip events that were already replayed (deduplication for race prevention)
			if notification.Commit <= lastReplayedCommit {
				continue
			}
			if scoped {
				// Lazily seed the diff baseline at the commit just before the first
				// event, so a queued race-window event (commit <= startCommit) yields a
				// correct forward delta instead of being folded into the baseline.
				if !prevSeeded {
					var err error
					prevDoc, err = s.scopedDocAt(path, notification.Commit-1)
					if err != nil {
						s.log.Error("failed to read scoped watch base", "path", path, "commit", notification.Commit-1, "error", err)
						s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read scoped state at commit %d: %v", notification.Commit-1, err))
						return
					}
					prevSeeded = true
				}
				// Recompute the scoped view at this commit and emit only the change
				// vs. the previously emitted state. notification.Patch (the raw
				// baseline/scope delta) is intentionally ignored.
				newPrev, err := s.emitScopedDelta(path, notification.Commit, prevDoc)
				if err != nil {
					s.log.Error("failed to read scoped state for watch", "path", path, "commit", notification.Commit, "error", err)
					s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read scoped state at commit %d: %v", notification.Commit, err))
					return
				}
				prevDoc = newPrev
				continue
			}
			// The patch has already been stripped of internal tags by the hub
			// (WatchHub.Broadcast), which hands each notification an independent
			// copy. Forwarding it is read-only.
			s.send(api.NewPatchEvent(notification.Commit, path, notification.Patch))
		}
	}
}

// scopedDocAt returns the scoped state document (root-rooted, the watched path's
// subtree) at the given commit, normalized so a nil/empty result becomes ir.Null()
// for diffing.
func (s *Session) scopedDocAt(path string, commit int64) (*ir.Node, error) {
	if commit == 0 {
		return ir.Null(), nil
	}
	doc, err := s.storage.ReadStateAt(path, commit, s.scope)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return ir.Null(), nil
	}
	return doc, nil
}

// emitScopedDelta recomputes the scoped view at commit and, if it differs from prev,
// sends a root-rooted delta (prev -> new) as a patch event. It returns the new
// document to use as the next diff base (unchanged prev when there is no delta, so a
// baseline write to a scope-overridden leaf emits nothing).
func (s *Session) emitScopedDelta(path string, commit int64, prev *ir.Node) (*ir.Node, error) {
	newDoc, err := s.scopedDocAt(path, commit)
	if err != nil {
		return prev, err
	}
	if newDoc.DeepEqual(prev) {
		return prev, nil
	}
	s.send(api.NewPatchEvent(commit, path, tony.Diff(prev, newDoc)))
	return newDoc, nil
}

// handleUnwatch handles unwatch requests.
func (s *Session) handleUnwatch(id *string, req *api.UnwatchRequest) {
	path := req.Path

	s.watchMu.Lock()
	watcher, exists := s.watches[path]
	if exists {
		delete(s.watches, path)
	}
	s.watchMu.Unlock()

	if !exists {
		s.sendError(id, api.ErrCodeNotWatching, fmt.Sprintf("not watching %q", path))
		return
	}

	// Unwatch from hub
	s.hub.Unwatch(watcher)

	s.send(api.NewUnwatchResponse(id, path))
}

// handleDeleteScope handles delete scope requests.
// Only baseline sessions (scope=nil) can delete scopes.
func (s *Session) handleDeleteScope(id *string, req *api.DeleteScopeRequest) {
	// Only baseline sessions can delete scopes
	if s.scope != nil {
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
	if s.scope != nil {
		s.sendError(id, api.ErrCodeInvalidMessage, "only baseline sessions can modify schema")
		return
	}

	commit, err := s.storage.StartMigration(req.Schema)
	if err != nil {
		if errors.Is(err, storage.ErrMigrationInProgress) {
			s.sendError(id, api.ErrCodeMigrationInProgress, err.Error())
		} else {
			s.sendError(id, "storage_error", fmt.Sprintf("failed to start migration: %v", err))
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
	if s.scope != nil {
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
				s.sendError(id, "storage_error", fmt.Sprintf("failed to complete migration: %v", err))
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
				s.sendError(id, "storage_error", fmt.Sprintf("failed to abort migration: %v", err))
			}
			return
		}
		s.send(api.NewMigrationResponse(id, false, commit))

	default:
		s.sendError(id, api.ErrCodeInvalidMessage, fmt.Sprintf("invalid migration action: %q", *action))
	}
}

// cleanupWatches removes all watches on session close.
func (s *Session) cleanupWatches() {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()

	for path, watcher := range s.watches {
		s.hub.Unwatch(watcher)
		delete(s.watches, path)
	}
}

// send queues a response for sending.
func (s *Session) send(resp *api.SessionResponse) {
	select {
	case s.outgoing <- resp:
	case <-s.done:
	}
}

// sendError sends an error response.
func (s *Session) sendError(id *string, code, message string) {
	s.send(api.NewErrorResponse(id, code, message))
}

// failWatch terminates a watch, sending an error to the client and cleaning up.
func (s *Session) failWatch(watcher *Watcher, code, message string) {
	s.send(api.NewErrorResponse(nil, code, message))
	s.hub.Unwatch(watcher)
	s.watchMu.Lock()
	delete(s.watches, watcher.Path)
	s.watchMu.Unlock()
}

// scopesEqual compares two scope pointers for equality.
// nil scopes are considered equal to each other.
func scopesEqual(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// scopeStr returns a display string for a scope pointer.
func scopeStr(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
