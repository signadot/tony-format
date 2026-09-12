package server

import (
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
	"github.com/signadot/tony-format/go-tony/token"
)

// Session represents a bidirectional session with a client.
// It handles parsing requests, dispatching to handlers, and sending responses/events.
type Session struct {
	// readBudget is the largest node this session builds for one read; see SessionConfig.
	readBudget int64

	ID      string
	conn    io.ReadWriteCloser
	storage *storage.Storage
	hub     *WatchHub
	log     *slog.Logger

	// Scope for COW isolation (set in hello, applies to all operations). Atomic
	// because hello lands on the request loop while reads dispatched off it are
	// running (see dispatch), and a client is free to say hello twice.
	scope atomic.Pointer[string]

	// author is the session's default writer (api.Hello.Author): what a patch naming
	// none is recorded as written by. Set by hello, on the request loop; read by a
	// joining patch, which runs off it.
	author atomic.Pointer[string]

	// refused says the last hello named a protocol this server does not speak. Every
	// request after it is refused the same way until a hello this server speaks: the
	// refusal was answered and then everything behind it served, so a pipelining client
	// had its writes committed by a server that had just told it they would not be
	// (mygs3chwh12ksyxxmdn0).
	refused atomic.Bool

	// readSlots bounds concurrent reads; readWG lets shutdown wait for the ones in
	// flight. Reads run off the request loop so a slow one does not hold up the
	// writes behind it -- see dispatch.
	readSlots chan struct{}
	readWG    sync.WaitGroup

	// Watch state
	watchMu sync.RWMutex
	watches map[string]*Watcher // watchKey(id, path) -> active watcher

	// Communication channels
	outgoing chan outbound // responses and events to send
	done     chan struct{} // signals session shutdown

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
	OnCommit       func() // called after successful commits (for snapshot tracking)
	OutgoingBuffer int    // buffer size for outgoing channel (default 100)
	// ReadBudget is the largest node a session builds to answer one read: a match, a
	// watch's initial state, a watch's read of the value at its path. A read past it is
	// refused with ErrBudget rather than held. Zero is DefaultReadBudget; every read
	// that builds a node states a number (read_write_interface.md), and this is the
	// session's.
	ReadBudget int64
}

// DefaultReadBudget is the read budget a session has when its configuration names none.
const DefaultReadBudget = 64 << 20

// NewSession creates a new session for the given connection.
func NewSession(id string, conn io.ReadWriteCloser, cfg *SessionConfig) *Session {
	bufSize := cfg.OutgoingBuffer
	if bufSize <= 0 {
		bufSize = 100
	}
	budget := cfg.ReadBudget
	if budget <= 0 {
		budget = DefaultReadBudget
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &Session{
		readBudget: budget,
		ID:         id,
		readSlots:  make(chan struct{}, maxConcurrentReads),
		conn:       conn,
		storage:    cfg.Storage,
		hub:        cfg.Hub,
		log:        log.With("session", id),
		watches:    make(map[string]*Watcher),
		outgoing:   make(chan outbound, bufSize),
		done:       make(chan struct{}),
		onCommit:   cfg.OnCommit,
	}
}

// Run serves the session and blocks until it completes: it reads and dispatches
// requests on the calling goroutine while a writer goroutine sends responses and events.
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

	// Reads dispatched off the loop may still be running; each ends by sending, which
	// selects on done, so this waits for them to notice rather than for their work.
	s.readWG.Wait()

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
		case out := <-s.outgoing:
			if out.frame != nil {
				// Encoded already, off the loop, from the store's event stream
				// (encodedMatch).
				if _, err := s.conn.Write(out.frame); err != nil {
					s.log.Error("failed to write response", "error", err)
					return
				}
				continue
			}
			// Use wire format to match client's WithBrackets() decoder. Comments
			// go with it: a store keeps what it is given (api.NextState), and a
			// read that dropped them on the way out would make that pointless.
			// The wire form stays compact except where a comment ends its line.
			data, err := out.resp.ToTony(api.WireOptions()...)
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

// outbound is one thing the writer sends: a response built as a node, or a frame
// encoded already from the store's event stream.
type outbound struct {
	resp  *api.SessionResponse
	frame []byte
}

// maxConcurrentReads bounds the reads one session may have in flight at once. A read
// materializes a document, so unbounded is a memory hazard on a store with a big root;
// beyond this the dispatch loop waits, which is what it always did.
const maxConcurrentReads = 8

// scopeID is this session's COW scope, or nil for the baseline. Read through here
// rather than off the field: hello writes it on the request loop while reads run
// beside it.
func (s *Session) scopeID() *string { return s.scope.Load() }

// authorFor is who a patch is recorded as written by: the author it names, else the
// session's (api.PatchRequest.Author). Empty is none.
func (s *Session) authorFor(req *api.PatchRequest) string {
	if req.Author != "" {
		return req.Author
	}
	if a := s.author.Load(); a != nil {
		return *a
	}
	return ""
}

// dispatch routes a request to the appropriate handler.
//
// Everything here runs ON the request loop -- the next request waits -- except a read,
// which does not. One client is one session (libctl dials once and shares it), so a
// read taking a second put every write behind it in the same line: a source trying to
// land a write waited on a status read of a document it had no interest in. Writes
// stay on the loop, which is what keeps a client's own ordering: a read dispatched
// after a write is dispatched after that write COMMITTED, so read-your-writes holds
// without anything being tracked. A read still running when a later write commits is
// concurrent, and says which commit it read at (7qayp3hah12kscx2gdn0).
//
// A ping stays on the loop deliberately: its answer means "this loop is alive", and a
// probe answered from elsewhere cannot say that.
func (s *Session) dispatch(req *api.SessionRequest) {
	if req.Hello == nil && s.refused.Load() {
		s.sendError(req.ID, api.ErrCodeProtocolMismatch,
			"this session was refused at hello: say hello with a protocol this server speaks")
		return
	}
	switch {
	case req.Hello != nil:
		s.handleHello(req.ID, req.Hello)
	case req.Match != nil:
		s.readSlots <- struct{}{}
		s.readWG.Add(1)
		go func() {
			defer func() { s.readWG.Done(); <-s.readSlots }()
			s.handleMatch(req.ID, req.Match)
		}()
	case req.Patch != nil:
		// A patch which JOINS a transaction waits for the other participants, and
		// blocking the loop makes that wait unsatisfiable: the participants a client
		// pipelines behind it cannot be read, so the transaction times out and every
		// one of them fails. It is not held here.
		//
		// Nothing is given up by that. A plain patch stays on the loop, which is what
		// keeps read-your-writes; a joining patch has nothing to be read back yet,
		// since its own write is not committed until the transaction is -- a client
		// which wants to see it must wait for the result either way.
		if req.Patch.TxID != nil {
			s.readWG.Add(1)
			go func() {
				defer s.readWG.Done()
				s.handlePatch(req.ID, req.Patch)
			}()
			return
		}
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
	case req.Ping != nil:
		// A liveness probe which also answers "where is the store now". The head is
		// a memory read (the tick watermark), so a client can keep a current revision
		// from the heartbeat it already sends rather than by holding a watch open for
		// its initial state -- which costs a full read and reports where the WATCH
		// starts, not where the store is (7qayp3hah12kscx2gdn0).
		head, err := s.storage.GetCurrentCommit()
		if err != nil {
			head = 0 // a probe answers; where the store is, is a lesser question
		}
		s.send(api.NewPongResponseAt(req.ID, head, s.storage.ReplayFloor()))
	default:
		s.sendError(req.ID, api.ErrCodeInvalidMessage, "no operation specified")
	}
}

// handleHello handles hello handshake.
func (s *Session) handleHello(id *string, req *api.Hello) {
	// A version this server does not speak is refused HERE, because it cannot be refused
	// later: an unknown request field is ignored, and an unread path is the root. See
	// api.ProtocolVersion.
	switch {
	case req.Protocol == 0:
		// A client from before this existed. Say so once, at the handshake, rather than
		// refusing a deployment which has not caught up.
		s.log.Info("client speaks no protocol version; assuming the current one",
			"clientId", req.ClientID, "protocol", api.ProtocolVersion)
	case req.Protocol != api.ProtocolVersion:
		s.log.Warn("refusing a session on a protocol this server does not speak",
			"clientId", req.ClientID, "client", req.Protocol, "server", api.ProtocolVersion)
		s.refused.Store(true)
		s.sendError(id, api.ErrCodeProtocolMismatch, fmt.Sprintf(
			"client speaks session protocol %d, server speaks %d: deploy them together",
			req.Protocol, api.ProtocolVersion))
		return
	}
	s.refused.Store(false)

	// Store scope for this session (applies to all operations), and the writer a
	// patch on it is recorded under when it names none.
	s.scope.Store(req.Scope)
	author := req.Author
	s.author.Store(&author)
	s.log.Debug("hello", "clientId", req.ClientID, "scope", req.Scope, "author", req.Author)

	// The store's schema: a configured one was committed to the store when it was
	// opened with none (storage.BootstrapSchema), so there is nothing to fall back to.
	schema, schemaCommit := s.storage.GetActiveSchema()

	s.send(&api.SessionResponse{
		ID: id,
		Result: &api.SessionResult{
			Hello: &api.HelloResponse{
				ServerID:     s.ID,
				Protocol:     api.ProtocolVersion,
				Schema:       schema,
				SchemaCommit: schemaCommit,
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

// send queues a response for sending.
func (s *Session) send(resp *api.SessionResponse) {
	select {
	case s.outgoing <- outbound{resp: resp}:
	case <-s.done:
	}
}

// sendFrame queues a frame encoded already.
func (s *Session) sendFrame(frame []byte) {
	select {
	case s.outgoing <- outbound{frame: frame}:
	case <-s.done:
	}
}

// sendError sends an error response.
func (s *Session) sendError(id *string, code, message string) {
	s.send(api.NewErrorResponse(id, code, message))
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
