package server

import (
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// Session represents a bidirectional session with a client.
// It handles parsing requests, dispatching to handlers, and sending responses/events.
type Session struct {
	ID      string
	conn    io.ReadWriteCloser
	storage *storage.Storage
	hub     *WatchHub
	log     *slog.Logger

	// Watch state
	watchMu sync.RWMutex
	watches map[string]*Watcher // path -> active watcher

	// Communication channels
	outgoing chan *api.SessionResponse // responses and events to send
	done     chan struct{}             // signals session shutdown

	// Shutdown coordination
	closeOnce     sync.Once
	closeOutOnce  sync.Once

	// For tracking commits since snapshot (shared with server)
	onCommit func()
}

// SessionConfig contains configuration for creating a session.
type SessionConfig struct {
	Storage       *storage.Storage
	Hub           *WatchHub
	Log           *slog.Logger
	OnCommit      func() // called after successful commits (for snapshot tracking)
	OutgoingBuffer int   // buffer size for outgoing channel (default 100)
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

	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.writer()
	}()

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
func (s *Session) reader() error {
	decoder, err := stream.NewDecoder(s.conn, stream.WithBrackets())
	if err != nil {
		return fmt.Errorf("failed to create decoder: %w", err)
	}

	for {
		select {
		case <-s.done:
			return nil
		default:
		}

		// Read a complete document (events until depth returns to 0)
		node, err := s.readDocument(decoder)
		if err != nil {
			if err == io.EOF {
				return nil
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
		data, err := resp.ToTony()
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
	case req.Watch != nil:
		s.handleWatch(req.ID, req.Watch)
	case req.Unwatch != nil:
		s.handleUnwatch(req.ID, req.Unwatch)
	default:
		s.sendError(req.ID, api.ErrCodeInvalidMessage, "no operation specified")
	}
}

// handleHello handles hello handshake.
func (s *Session) handleHello(id *string, req *api.Hello) {
	s.log.Debug("hello", "clientId", req.ClientID)
	s.send(&api.SessionResponse{
		ID: id,
		Result: &api.SessionResult{
			Hello: &api.HelloResponse{
				ServerID: s.ID,
			},
		},
	})
}

// handleMatch handles match (read) requests.
func (s *Session) handleMatch(id *string, req *api.MatchRequest) {
	path := req.Body.Path

	// Validate path
	if err := validateDataPath(path); err != nil {
		s.sendError(id, api.ErrCodeInvalidPath, err.Error())
		return
	}

	// Get current commit
	commit, err := s.storage.GetCurrentCommit()
	if err != nil {
		s.sendError(id, "storage_error", fmt.Sprintf("failed to get current commit: %v", err))
		return
	}

	// Read state
	doc, err := s.storage.ReadStateAt(path, commit)
	if err != nil {
		s.sendError(id, "storage_error", fmt.Sprintf("failed to read state: %v", err))
		return
	}

	// Extract value at path
	state, err := extractPathValue(doc, path)
	if err != nil {
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
func (s *Session) handlePatch(id *string, req *api.PatchRequest) {
	path := req.Patch.Path

	// Validate path
	if err := validateDataPath(path); err != nil {
		s.sendError(id, api.ErrCodeInvalidPath, err.Error())
		return
	}

	// Validate patch data
	if req.Patch.Data == nil {
		s.sendError(id, api.ErrCodeInvalidDiff, "patch data is required")
		return
	}

	// Create single-participant transaction
	tx, err := s.storage.NewTx(1, nil)
	if err != nil {
		s.sendError(id, "storage_error", fmt.Sprintf("failed to create transaction: %v", err))
		return
	}

	// Create patcher and commit
	patcher, err := tx.NewPatcher(&api.Patch{
		Patch: req.Patch,
	})
	if err != nil {
		s.sendError(id, "storage_error", fmt.Sprintf("failed to create patcher: %v", err))
		return
	}

	result := patcher.Commit()
	if result.Error != nil {
		s.sendError(id, "storage_error", fmt.Sprintf("failed to commit: %v", result.Error))
		return
	}

	// Notify server for snapshot tracking
	if s.onCommit != nil {
		s.onCommit()
	}

	s.send(api.NewPatchResponse(id, result.Commit))
}

// handleWatch handles watch requests.
func (s *Session) handleWatch(id *string, req *api.WatchRequest) {
	path := req.Path

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
	watcher := NewWatcher(path, req.FromCommit, req.FullState, 100)
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
	go s.forwardEvents(watcher, req.FromCommit, req.FullState, currentCommit)
}

// forwardEvents forwards events from a watcher to the session's outgoing channel.
// It handles replay if needed, then forwards live events with deduplication.
//
// Race prevention: We registered with the hub BEFORE getting currentCommit.
// This means events that arrive between Watch and GetCurrentCommit are queued.
// After replay completes, we skip any queued events with commit <= currentCommit
// since they were already replayed.
func (s *Session) forwardEvents(watcher *Watcher, fromCommit *int64, fullState bool, currentCommit int64) {
	path := watcher.Path

	// Track the highest commit we've replayed (for deduplication)
	lastReplayedCommit := int64(0)

	// Handle replay if fromCommit is specified
	if fromCommit != nil {
		startCommit := *fromCommit
		lastReplayedCommit = currentCommit

		// If fullState requested, send state at fromCommit first
		if fullState {
			state, err := s.storage.ReadStateAt(path, startCommit)
			if err != nil {
				s.log.Error("failed to read state for replay", "path", path, "commit", startCommit, "error", err)
			} else {
				// Extract value at path if needed
				if path != "" {
					state, err = extractPathValue(state, path)
					if err != nil {
						s.log.Error("failed to extract path value for replay", "path", path, "error", err)
						state = ir.Null()
					}
				}
				s.send(api.NewStateEvent(startCommit, path, state))
			}
		}

		// Send historical patches from startCommit+1 to currentCommit
		if startCommit < currentCommit {
			patches, err := s.storage.ReadPatchesInRange(path, startCommit+1, currentCommit)
			if err != nil {
				s.log.Error("failed to read patches for replay", "path", path, "from", startCommit+1, "to", currentCommit, "error", err)
			} else {
				for _, patch := range patches {
					s.send(api.NewPatchEvent(patch.Commit, path, patch.Patch))
				}
			}
		}

		s.send(api.NewReplayCompleteEvent())
	}

	// Forward live events, skipping any already replayed
	for {
		select {
		case <-s.done:
			return
		case <-watcher.Failed:
			// Watch failed (slow consumer)
			s.log.Warn("watch failed (slow consumer)", "path", path)
			s.send(api.NewErrorResponse(nil, api.ErrCodeSessionClosed, fmt.Sprintf("watch on %q failed: slow consumer", path)))
			// Remove from our tracking
			s.watchMu.Lock()
			delete(s.watches, path)
			s.watchMu.Unlock()
			return
		case notification, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Skip events that were already replayed (deduplication for race prevention)
			if notification.Commit <= lastReplayedCommit {
				continue
			}
			// Convert notification to event
			s.send(api.NewPatchEvent(notification.Commit, path, notification.Patch))
		}
	}
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
