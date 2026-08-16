package server

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
	"github.com/signadot/tony-format/go-tony/token"
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
	closeOnce sync.Once

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

	// The writer exits on s.done (closed above); outgoing is intentionally never
	// closed so a shutdown-racing send() cannot panic on a closed channel.

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
		node, err := stream.ReadDocument(decoder)
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

// writer sends outgoing responses and events. It exits when the session is done
// rather than when outgoing is closed: outgoing is deliberately never closed, so
// that a late send() from a forwardEvents/failWatch goroutine racing shutdown can
// never panic with "send on closed channel" (send selects on outgoing vs done, and
// a select with both channels ready may pick the closed send). Losing any buffered
// responses on a closing session is harmless — the peer is already gone.
func (s *Session) writer() {
	for {
		select {
		case <-s.done:
			return
		case resp := <-s.outgoing:
			// Use wire format to match client's WithBrackets() decoder. Comments
			// go with it: a store keeps what it is given (api.NextState), and a
			// read that dropped them on the way out would make that pointless.
			// The wire form stays compact except where a comment ends its line.
			data, err := resp.ToTony(api.WireOptions()...)
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
	case req.Ping != nil:
		s.send(api.NewPongResponse(req.ID)) // liveness probe
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

	// Extract value at path. A bad segment is the client's path being wrong, not
	// its data being missing: reporting it as not-found reads as "nothing there
	// yet" and invites a retry that can never succeed.
	state, err := extractPathValue(doc, path)
	if err != nil {
		var pe *PathError
		if errors.As(err, &pe) && pe.Kind == PathBadSegment {
			s.sendError(id, api.ErrCodeInvalidPath, err.Error())
			return
		}
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
		// A path which names no array element is the client's mistake, and it is the
		// same mistake next time: reporting it as a storage_error (or, in a
		// transaction, as tx_full) tells the client to retry something that cannot
		// work, and hides which path was wrong.
		var noElem *tx.NoSuchElementError
		if errors.As(err, &noElem) {
			s.sendError(id, api.ErrCodeInvalidPath, err.Error())
			return
		}
		// An operation which executes is the patch's problem, not the path's, and it
		// is the same problem next time: the client is told what it wrote, not that
		// the store failed.
		var unsafeOp *tx.UnsafeOpError
		if errors.As(err, &unsafeOp) {
			s.sendError(id, api.ErrCodeInvalidDiff, err.Error())
			return
		}
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
		// The write was accepted and the array lost the element before it committed;
		// the store is healthy and the client's path is the thing that is wrong now.
		var noElem *tx.NoSuchElementError
		if errors.As(result.Error, &noElem) {
			s.sendError(id, api.ErrCodeInvalidPath, result.Error.Error())
			return
		}
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

// watchKey is the s.watches map key for a watch. An id-bearing watch is keyed by
// its (session-unique) request id, so several watches on the same path coexist; an
// id-less (legacy) watch is keyed by path, of which there is at most one. The
// "id:"/"path:" prefixes keep the two namespaces from colliding.
func watchKey(id *string, path string) string {
	if id != nil {
		return "id:" + *id
	}
	return "path:" + path
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

	// Admission: a path is either a single id-less watch or N distinct id-bearing
	// watches, never mixed, so events route unambiguously. Reject an id-less watch
	// when the path is already watched, an id-bearing watch when an id-less watch
	// holds the path, and any request whose id duplicates an existing watch.
	s.watchMu.RLock()
	var reject string
	for _, w := range s.watches {
		if id != nil && w.ID != nil && *w.ID == *id {
			reject = fmt.Sprintf("already watching with id %q", *id)
			break
		}
		if w.Path != path {
			continue
		}
		if id == nil {
			reject = fmt.Sprintf("already watching %q", path)
			break
		}
		if w.ID == nil {
			reject = fmt.Sprintf("%q already has an id-less watch", path)
			break
		}
	}
	s.watchMu.RUnlock()
	if reject != "" {
		s.sendError(id, api.ErrCodeAlreadyWatching, reject)
		return
	}

	// IMPORTANT: Register with hub FIRST to avoid race condition.
	// Events that arrive between Watch and GetCurrentCommit will be queued.
	// After replay, we skip any queued events with commit <= currentCommit.
	// Buffer sized for burst tolerance: Broadcast is non-blocking and fails a watcher whose
	// buffer is full (see WatchHub.Broadcast), so the buffer — not a time grace — is what
	// absorbs a transient read stall before the watch is failed.
	watcher := NewWatcher(path, s.scope, req.FromCommit, 1024)
	watcher.ID = id
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
	s.watches[watchKey(id, path)] = watcher
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

	// The highest commit this watch has accounted for, handed to the client on a terminal
	// event as its resume point. It advances even for a commit that produced no event for
	// this path — the watch is correct through that commit, so resuming above it skips
	// replaying history the client already has. Zero until the watch has caught up to
	// anything, which is what a client that never got started should resume from.
	lastDelivered := int64(0)

	// Determine the starting commit for initial state
	startCommit := currentCommit
	if fromCommit != nil {
		startCommit = *fromCommit
		lastReplayedCommit = currentCommit
	}

	// Refuse a cursor below the retained delta window before sending ANYTHING. The
	// replay itself would catch this (ReadPatchesInRange returns ErrReplayCompacted), but
	// only after the initial state has gone out — and a state read below the floor is
	// itself approximate, since compaction leaves historical reads at snapshot
	// granularity. Handing the client a state it cannot trust and then an error is worse
	// than the error alone.
	//
	// The bound matches the replay's: it reads [startCommit+1, ...], so a cursor AT the
	// floor is fine — "I have through commit F" needs only the deltas above F, which are
	// intact — and one below it is not.
	if fromCommit != nil {
		if floor := s.storage.ReplayFloor(); *fromCommit < floor {
			s.log.Warn("watch cursor below retained history", "path", path, "fromCommit", *fromCommit, "floor", floor)
			s.failWatch(watcher, api.ErrCodeReplayCompacted, fmt.Sprintf(
				"cannot replay from commit %d: delta history is retained only from commit %d; re-watch without fromCommit to re-initialize",
				*fromCommit, floor+1), 0)
			return
		}
	}

	// A watch whose path has no value yet is the ordinary way to start watching
	// something that does not exist. Say it once, quietly, and say so again when
	// it arrives: the pair is a story an operator can follow, where the same line
	// repeated per event is just an alarm they learn to ignore.
	absent := &watchAbsence{log: s.log, path: path}

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
				s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read state at commit %d: %v", startCommit, err), lastDelivered)
				return
			}
			// Extract value at path if needed.
			//
			// A failure here says nothing about storage: the read above already
			// succeeded, and this is navigation of the document it returned. What
			// it says is which of three things is true, and they want three
			// different volumes -- see PathErrorKind.
			if path != "" {
				state, err = extractPathValue(state, path)
				if err != nil {
					var pe *PathError
					switch {
					case errors.As(err, &pe) && pe.Kind == PathBadSegment:
						// This one never resolves, so serving null forever would
						// tell the client its path is empty when it is invalid.
						s.log.Warn("watch path cannot be extracted", "path", path, "error", err)
						s.failWatch(watcher, api.ErrCodeInvalidPath, err.Error(), lastDelivered)
						return
					case errors.As(err, &pe) && pe.Kind == PathTypeConflict:
						s.log.Warn("watched path is shadowed by a non-object", "path", path, "error", err)
					default:
						s.log.Debug("watched path has no value yet", "path", path, "detail", err.Error())
					}
					state = ir.Null()
					absent.arm()
				}
			}
		}
		s.send(api.NewStateEvent(watcher.ID, startCommit, path, state))
		lastDelivered = startCommit
	}

	// prevDoc tracks the watched path's own subtree (as scopedDocAt trims it) at the
	// last delivered commit. A scoped watcher uses it to emit deltas by
	// recompute-and-diff (it must reproduce the scoped read at each commit; a raw
	// committed delta does not, because scope writes shadow baseline and !key merges
	// are identity-based). A BASELINE watcher uses it only as a change GATE: a coarse
	// wake (top-level KPath) plus the superset read can wake a watcher for a commit
	// that only touched a sibling under a shared ancestor, so before forwarding the
	// raw committed delta (which baseline keeps for op fidelity) we confirm the
	// watcher's own subtree actually changed. See issue eagjggjdh12ksg00bsn0.
	//
	// prevDoc is seeded at startCommit for a fromCommit replay (the replay below diffs
	// or gates forward from it). For a no-replay watcher it is seeded LAZILY, at
	// (firstEvent.commit - 1) when the first event arrives — never at startCommit —
	// because a write can race into [hub-register, GetCurrentCommit] and be queued with
	// commit <= startCommit; seeding at startCommit would fold that write into the
	// baseline and drop its delta (the scoped-watch drop-one-event regression). Lazy
	// seeding makes every queued or live event a correct forward diff.
	//
	// A BASELINE watcher additionally keeps curDoc, the whole document at that same
	// commit, and STEPS it: curDoc = Patch(curDoc, committedPatch), then trims to get the
	// subtree. That is the read path's own fold (processor.go applies patches with
	// tony.Patch, and read_equivalence_test's oracle calls the fold from commit 0 "the
	// semantics of record"), just not restarted from a snapshot every time. It replaces a
	// full ReadStateAt per event per watcher, which was O(patches since the last
	// snapshot): 1.6ms at 50 commits, 62ms at 1550, paid again by every watcher on every
	// commit that reached it. Nothing is kept that was not already built — the old code
	// materialized a whole document per event and threw it away.
	//
	// A SCOPED watcher cannot step: its view is baseline with the scope's own writes
	// applied LAST, so they shadow baseline stickily, and applying a baseline patch to a
	// materialized scoped document would let a baseline write overwrite a leaf the scope
	// owns. It keeps recompute-and-diff until the scope layer gets the same treatment.
	var prevDoc *ir.Node
	var curDoc *ir.Node
	// A scoped watcher's stepper, seeded with prevDoc. nil means recompute per event, which
	// is what every scoped watcher did before and what a scope the overlay cannot serve
	// still does.
	var stepper *storage.ScopedWatchStepper
	prevSeeded := false
	if fromCommit != nil {
		var err error
		if scoped {
			prevDoc, err = s.scopedDocAt(path, startCommit)
		} else {
			curDoc, err = s.fullDocAt(startCommit)
			prevDoc = subtreeOf(curDoc, path)
		}
		if err != nil {
			s.log.Error("failed to read watch base", "path", path, "commit", startCommit, "error", err)
			s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read state at commit %d: %v", startCommit, err), lastDelivered)
			return
		}
		prevSeeded = true
	}

	// Handle replay if fromCommit is specified
	if fromCommit != nil {
		// Send historical patches from startCommit+1 to currentCommit
		if startCommit < currentCommit {
			patches, err := s.storage.ReadPatchesInRange(path, startCommit+1, currentCommit, s.scope)
			if errors.Is(err, storage.ErrReplayCompacted) {
				// The cursor predates the retained delta window (compaction cutoff), so
				// the exact history it asked for no longer exists. Say that specifically:
				// a client told "replay_compacted" re-watches without fromCommit and
				// re-initializes from current state, where "replay_failed" reads as a
				// transient fault worth retrying with the same doomed cursor.
				s.log.Warn("watch replay below retained history", "path", path, "fromCommit", startCommit, "error", err)
				s.failWatch(watcher, api.ErrCodeReplayCompacted,
					fmt.Sprintf("cannot replay from commit %d: %v; re-watch without fromCommit to re-initialize", startCommit, err), lastDelivered)
				return
			}
			if err != nil {
				s.log.Error("failed to read patches for replay", "path", path, "from", startCommit+1, "to", currentCommit, "error", err)
				s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read patches from commit %d to %d: %v", startCommit+1, currentCommit, err), lastDelivered)
				return
			}
			for _, patch := range patches {
				if scoped {
					newPrev, err := s.emitScopedDelta(watcher.ID, path, patch.Commit, prevDoc)
					if err != nil {
						s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read scoped state at commit %d: %v", patch.Commit, err), lastDelivered)
						return
					}
					prevDoc = newPrev
					absent.observe(prevDoc)
					lastDelivered = patch.Commit
					continue
				}
				// Baseline: forward the raw delta (op fidelity), but only if this
				// watcher's own subtree actually changed at this commit — the range
				// read can include a sibling's write under a shared ancestor.
				//
				// Step rather than re-read. The tags are stripped first because they are
				// the streaming processor's patch-root markers, not part of the value; the
				// send below strips for the same reason.
				tx.StripPatchRootTagRecursive(patch.Patch)
				stepped, err := api.NextState(curDoc, patch.Patch)
				if err != nil {
					s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to apply patch at commit %d: %v", patch.Commit, err), lastDelivered)
					return
				}
				curDoc = stepped
				newSub := subtreeOf(curDoc, path)
				// Accounted for either way: a commit that changed nothing under this path
				// still leaves the watch correct through it, so it is a valid resume point.
				lastDelivered = patch.Commit
				// api.SameState decides what counts as a change; see it for comments.
				if api.SameState(newSub, prevDoc) {
					continue
				}
				prevDoc = newSub
				absent.observe(prevDoc)
				s.send(api.NewPatchEvent(watcher.ID, patch.Commit, path, patch.Patch))
			}
		}

		s.send(api.NewReplayCompleteEvent(watcher.ID, path))
	}

	// Forward live events, skipping any already replayed
	for {
		select {
		case <-s.done:
			return
		case <-watcher.Failed:
			// Broadcast dropped this watcher because its buffer was full. Report it as
			// what it is: the client fell behind, and it can resume from lastDelivered
			// rather than re-reading the whole document.
			s.failWatch(watcher, api.ErrCodeSlowConsumer,
				fmt.Sprintf("watch on %q dropped: consumer did not keep up", path), lastDelivered)
			return
		case notification, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Skip events that were already replayed (deduplication for race prevention)
			if notification.Commit <= lastReplayedCommit {
				continue
			}
			// Cheap pre-filter: the coarse wake fires this watcher for every write
			// under a shared top-level subtree, but recomputing full state per event
			// is O(log) and collapses under many watches. If the committed delta
			// clearly cannot reach this watcher's subtree (a plain merge that misses
			// the path), skip the recompute. Conservative: any op tag or a
			// non-navigable ancestor falls through to the authoritative recompute.
			if !patchMayAffect(notification.Patch, path) {
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
						s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read scoped state at commit %d: %v", notification.Commit-1, err), lastDelivered)
						return
					}
					prevSeeded = true

					// Seed a stepper at the same commit. From here the scoped view is
					// derived by folding each event into a baseline document the watcher
					// keeps -- the same move a baseline watcher already makes -- instead
					// of recomputing the whole view per event. It returns nil when the
					// scope cannot be served that way, and then nothing below changes.
					stepper, err = s.storage.NewScopedWatchStepper(*s.scope, notification.Commit-1)
					if err != nil {
						s.log.Warn("scoped watch stepper unavailable; recomputing per event",
							"path", path, "error", err)
						stepper = nil
					}
				}
				// Recompute the scoped view at this commit and emit only the change
				// vs. the previously emitted state. notification.Patch (the raw
				// baseline/scope delta) is intentionally ignored -- except by the
				// stepper, which folds it.
				newPrev, err := s.emitScopedDeltaStepped(watcher.ID, path, notification.Commit, prevDoc, stepper, notification)
				if err != nil {
					s.log.Error("failed to read scoped state for watch", "path", path, "commit", notification.Commit, "error", err)
					s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read scoped state at commit %d: %v", notification.Commit, err), lastDelivered)
					return
				}
				prevDoc = newPrev
				absent.observe(prevDoc)
				continue
			}
			// Baseline: forward the raw committed delta (already tag-stripped by the
			// hub, so read-only) to preserve op fidelity (!key etc.), but only if this
			// watcher's own subtree actually changed. A coarse wake plus the superset
			// read can deliver a commit that only touched a sibling under a shared
			// ancestor; the gate suppresses it. prevDoc is seeded lazily as scoped does.
			if !prevSeeded {
				var err error
				curDoc, err = s.fullDocAt(notification.Commit - 1)
				if err != nil {
					s.log.Error("failed to read watch base", "path", path, "commit", notification.Commit-1, "error", err)
					s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read state at commit %d: %v", notification.Commit-1, err), lastDelivered)
					return
				}
				prevDoc = subtreeOf(curDoc, path)
				prevSeeded = true
			}
			// Step the document by this commit's delta instead of rebuilding it from the
			// last snapshot. notification.Patch is already the tick's private, stripped
			// copy, so it can be applied as-is and is not mutated by Patch.
			stepped, err := api.NextState(curDoc, notification.Patch)
			if err != nil {
				s.log.Error("failed to apply patch for watch", "path", path, "commit", notification.Commit, "error", err)
				s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to apply patch at commit %d: %v", notification.Commit, err), lastDelivered)
				return
			}
			curDoc = stepped
			newSub := subtreeOf(curDoc, path)
			// Accounted for whether or not it changed anything here (see the replay loop).
			lastDelivered = notification.Commit
			// api.SameState decides what counts as a change; see it for comments.
			if api.SameState(newSub, prevDoc) {
				continue
			}
			prevDoc = newSub
			absent.observe(prevDoc)
			// The hub broadcasts ONE shared notification.Patch to every watcher on the
			// path (across sessions); encoding mutates a node's parent linkage
			// (ir.FromMap), so two session writers serializing the same node race. Hand
			// this watcher its own copy — the shared node is then only ever read.
			s.send(api.NewPatchEvent(watcher.ID, notification.Commit, path, notification.Patch.DeepCopy()))
		}
	}
}

// fullDocAt reads the whole document at a commit, normalized so an empty store is
// ir.Null(). It is the seed for a stepped watch (see forwardEvents): the one O(history)
// read a watch pays, after which each commit costs one patch application instead.
func (s *Session) fullDocAt(commit int64) (*ir.Node, error) {
	if commit <= 0 {
		return ir.Null(), nil
	}
	doc, err := s.storage.ReadStateAt("", commit, s.scope)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return ir.Null(), nil
	}
	return doc, nil
}

// subtreeOf trims a document to the watched path, with scopedDocAt's normalization —
// an absent or null subtree becomes ir.Null() so the change gate can compare it. It is
// scopedDocAt's second half, separated so a stepped document can be trimmed without
// being re-read.
func subtreeOf(doc *ir.Node, path string) *ir.Node {
	if doc == nil {
		return ir.Null()
	}
	sub, err := extractPathValue(doc, path)
	if err != nil {
		return ir.Null() // path absent in this commit's state
	}
	if sub == nil || sub.Type == ir.NullType {
		return ir.Null()
	}
	return sub
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
	// ReadStateAt returns a rooted SUPERSET: it collects ancestor-level index
	// segments and applies whole patch entries, so a read of "p.b" carries a
	// sibling "p.a" write done under the shared ancestor "p". Trim to the path's
	// own subtree (mirroring handleMatch and watch-init, which extract after
	// reading) so a scoped watcher does not see a sibling's write as a change.
	sub, err := extractPathValue(doc, path)
	if err != nil {
		return ir.Null(), nil // path absent in this commit's state
	}
	if sub == nil || sub.Type == ir.NullType {
		return ir.Null(), nil
	}
	return sub, nil
}

// patchMayAffect reports whether the committed delta could change the subtree at
// path. It is a cheap structural pre-filter that lets a watcher skip the expensive
// full-state recompute for the common case — a sibling write, under a shared
// ancestor, that does not reach the watched path. It is CONSERVATIVE: it returns
// true (fall through to the authoritative recompute) for any op-tagged node along
// the path (an op's effect can extend beyond its structural location — !replace,
// !delete, !key, !arraydiff, ...) or a non-navigable ancestor. It returns false
// only for a plain merge that structurally misses the path — a merge only adds or
// overwrites the keys it names, so if it never reaches the path, the subtree is
// unchanged. Soundness rests on the recompute, not on this filter.
//
// Segment matching MUST normalize quoting, mirroring the read side's path handling
// (docd pathFields): SplitAll yields a digit-first key as the quoted "9reprokind",
// but the field is stored as the unquoted 9reprokind. SegmentFieldName unquotes the
// path segment to its canonical field name; the field key is matched in either
// stored form. Getting this wrong (comparing the quoted segment verbatim, or
// unquoting only one side) makes every quoted key — decision ids are %08x, ~62%
// digit-first — miss its field and silently drops the watcher's events.
func patchMayAffect(patch *ir.Node, path string) bool {
	if patch == nil {
		return false
	}
	if path == "" {
		return true
	}
	names, ok := pathFieldNames(path)
	if !ok {
		return true // malformed, or a non-field segment (array/sparse index)
	}
	cur := patch
	for _, name := range names {
		if cur == nil {
			return false
		}
		// A genuine merge operation (!replace, !delete, !key, ...) can change the
		// subtree even when structural navigation would miss it, so any op tag along
		// the path falls through to the authoritative recompute. Presentation tags
		// (!bracket, ...) are NOT operations and must be ignored — SplitChild strips
		// them exactly as the patch applier does. Using cur.Tag != "" instead would
		// treat every bracket-encoded node as an op and defeat the filter entirely.
		if _, op, _, _, err := mergeop.SplitChild(cur); err != nil || op != "" {
			return true
		}
		if cur.Type != ir.ObjectType {
			// A non-object ancestor (array/scalar), or a non-field segment such as
			// an array index that the read side does not navigate structurally —
			// be conservative and let the recompute decide.
			return true
		}
		next := getField(cur, name)
		if next == nil {
			return false // a plain-merge sibling: this subtree is untouched
		}
		cur = next
	}
	return cur != nil // a node exists at the watched path — it was touched
}

// pathFieldNames splits path into canonical (unquoted) field names in a single
// kpath.Parse — which already unquotes each field segment (a digit-first key parses
// to the bare 9reprokind). It reports ok=false for a malformed path or any non-field
// segment (array/sparse index/wildcard) that cannot be navigated structurally, so
// the caller falls back to the authoritative recompute. This runs on every watch
// event, so it must stay a single Parse — do NOT reparse per segment.
func pathFieldNames(path string) ([]string, bool) {
	kp, err := kpath.Parse(path)
	if err != nil {
		return nil, false
	}
	var names []string
	for ; kp != nil; kp = kp.Next {
		if kp.Field == nil {
			return nil, false // a non-field segment (array/sparse index/wildcard)
		}
		names = append(names, *kp.Field)
	}
	return names, true
}

// getField looks up the field whose key equals name (already the unquoted canonical
// form from pathFieldNames), comparing against the field key in either stored form:
// the unquoted key directly (parse stores keys unquoted), or the key unquoted via a
// cheap quote strip (in case a producer stored it quoted). Biasing toward a match is
// safe — a false match only forces the authoritative recompute; a false miss would
// wrongly drop the event. Kept allocation- and Parse-free: it runs per field per
// event on the hot watch-delivery path.
func getField(node *ir.Node, name string) *ir.Node {
	for i, field := range node.Fields {
		if field.String == name || unquoteFieldKey(field.String) == name {
			return node.Values[i]
		}
	}
	return nil
}

// unquoteFieldKey strips surrounding quotes from a stored field key, mirroring the
// field-name branch of kpath's segment parser. A bare key is returned unchanged, so
// this is safe to call on any key and avoids a full kpath.Parse.
//
// token.Unquote validates before it decodes. The shape test this used to do instead —
// opens with a quote, ends with the same one — admits keys that are not well-formed
// quoted strings (`"a"b"`, `"a\qb"`), and the decoder used to panic on exactly those.
// Anything Unquote rejects is not a quoted key, so the raw key is the right answer.
func unquoteFieldKey(s string) string {
	if u, err := token.Unquote(s); err == nil {
		return u
	}
	return s
}

// emitScopedDelta recomputes the scoped view of the watched path's subtree at
// commit and, if it differs from prev, sends a root-rooted delta (prev -> new) as a
// patch event. prev and the returned value are the path's own subtree (as
// scopedDocAt trims it), so the diff is taken at the path and then re-rooted to the
// document root for the watch delta contract. It returns the new subtree to use as
// the next diff base (unchanged prev when there is no delta, so a baseline write to
// a scope-overridden leaf — or any sibling write — emits nothing).
// emitScopedDeltaStepped is emitScopedDelta with the option of a stepper. With one, the
// scope's document is folded from the committed patch rather than read back; without one,
// this is exactly the old path.
//
// The delta is still recompute-and-diff against the previous emitted state: a scope's raw
// committed patch is not its delta, because scope writes shadow baseline stickily and
// !key merges are identity-based. What the stepper removes is the READ, not the diff.
func (s *Session) emitScopedDeltaStepped(id *string, path string, commit int64, prev *ir.Node,
	stepper *storage.ScopedWatchStepper, n *storage.CommitNotification) (*ir.Node, error) {
	if stepper == nil {
		return s.emitScopedDelta(id, path, commit, prev)
	}
	full, err := stepper.Step(n)
	if err != nil {
		return prev, err
	}
	return s.emitScopedDeltaFrom(id, path, commit, prev, subtreeOf(full, path))
}

func (s *Session) emitScopedDelta(id *string, path string, commit int64, prev *ir.Node) (*ir.Node, error) {
	newDoc, err := s.scopedDocAt(path, commit)
	if err != nil {
		return prev, err
	}
	return s.emitScopedDeltaFrom(id, path, commit, prev, newDoc)
}

// emitScopedDeltaFrom sends the change between prev and newDoc, both already trimmed to
// the watched path.
func (s *Session) emitScopedDeltaFrom(id *string, path string, commit int64, prev, newDoc *ir.Node) (*ir.Node, error) {
	// What counts as a change is api.SameState's to say, here and at the two watch
	// paths above and the head's agreement check in storage/head.go. See it for why
	// the answer counts comments.
	if api.SameState(newDoc, prev) {
		return prev, nil
	}
	// The delta carries what the equality counts, or the two disagree in the other
	// direction: a change SameState reports would be diffed away to nothing and the
	// watcher told a commit happened by a patch that changes nothing. Inert on a
	// document with no comments, like the equality above it.
	rooted, err := tx.RootPatchAt(path, tony.DiffWith(prev, newDoc, tony.DiffComments(true)))
	if err != nil {
		return prev, err
	}
	s.send(api.NewPatchEvent(id, commit, path, rooted))
	return newDoc, nil
}

// handleUnwatch handles unwatch requests.
func (s *Session) handleUnwatch(id *string, req *api.UnwatchRequest) {
	path := req.Path

	// req.WatchID targets one specific watch; without it, cancel every watch on the
	// path (the legacy id-less behavior, and a bulk unwatch).
	s.watchMu.Lock()
	var removed []*Watcher
	if req.WatchID != nil {
		key := watchKey(req.WatchID, path)
		if w, ok := s.watches[key]; ok {
			removed = append(removed, w)
			delete(s.watches, key)
		}
	} else {
		for k, w := range s.watches {
			if w.Path == path {
				removed = append(removed, w)
				delete(s.watches, k)
			}
		}
	}
	s.watchMu.Unlock()

	if len(removed) == 0 {
		s.sendError(id, api.ErrCodeNotWatching, fmt.Sprintf("not watching %q", path))
		return
	}

	for _, w := range removed {
		s.hub.Unwatch(w)
	}

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

	for key, watcher := range s.watches {
		s.hub.Unwatch(watcher)
		delete(s.watches, key)
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

// failWatch terminates an established watch and tells the client, so it can re-establish.
//
// It sends a TERMINAL WATCH EVENT (Ended, with reason), not an error response. The
// distinction is the difference between the client finding out and not, and the failure
// it exists for is silent and not hypothetical: measured, a slice taking sustained writes
// lost 550 of 1000 events and never recovered. The path was —
//
//  1. logd fails a watcher whose buffer it cannot drain: Broadcast runs on the tick's
//     dispatcher and will not block on a slow consumer, so a full buffer drops the
//     watcher (see WatchHub.Broadcast, "fail it loudly").
//  2. "Loudly" meant an error response stamped with the watch's id.
//  3. libctl's read pump sends anything with no Event to deliverResponse, which looks the
//     id up in the table of in-flight REQUESTS. A watch id was never in that table — its
//     request completed when the watch opened — so the failure was logged as "dropping
//     response with no matching request" and thrown away.
//
// The client was then waiting on a watch the server had already abandoned, with no error
// and no events, forever. routeEvent handles Ended correctly and always did (it fails the
// Watch with a WatchEndedError and unregisters it); logd was the only sender not using it,
// while docd had been sending terminal events for mount-membership changes all along.
//
// An error response remains right for rejecting a watch REQUEST that is still in flight —
// handleWatch's admission checks — because that id is in the pending table.
//
// commit is the highest commit this watch accounted for, so the client can resume from it
// rather than re-reading everything; 0 when it never got that far. message is for the
// server log, since the terminal event carries a short reason code and no prose.
func (s *Session) failWatch(watcher *Watcher, reason, message string, commit int64) {
	s.log.Warn("watch ended", "path", watcher.Path, "reason", reason, "detail", message, "commit", commit)
	// Stamp the watch id so the client fails the right watch (several may share a path).
	s.send(api.NewEndedEvent(watcher.ID, watcher.Path, reason, commit))
	s.hub.Unwatch(watcher)
	s.watchMu.Lock()
	delete(s.watches, watchKey(watcher.ID, watcher.Path))
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
