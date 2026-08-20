package server

import (
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
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

	// Scope for COW isolation (set in hello, applies to all operations). Atomic
	// because hello lands on the request loop while reads dispatched off it are
	// running (see dispatch), and a client is free to say hello twice.
	scope atomic.Pointer[string]

	// If true, session uses pending schema/index (for testing migrations)
	// usePending is set by hello, like scope, and read by requests running beside the
	// loop -- atomic for the same reason.
	usePending atomic.Bool

	// readSlots bounds concurrent reads; readWG lets shutdown wait for the ones in
	// flight. Reads run off the request loop so a slow one does not hold up the
	// writes behind it -- see dispatch.
	readSlots chan struct{}
	readWG    sync.WaitGroup

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
		ID:        id,
		readSlots: make(chan struct{}, maxConcurrentReads),
		conn:      conn,
		storage:   cfg.Storage,
		hub:       cfg.Hub,
		log:       log.With("session", id),
		schema:    cfg.Schema,
		watches:   make(map[string]*Watcher),
		outgoing:  make(chan *api.SessionResponse, bufSize),
		done:      make(chan struct{}),
		onCommit:  cfg.OnCommit,
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

// maxConcurrentReads bounds the reads one session may have in flight at once. A read
// materializes a document, so unbounded is a memory hazard on a store with a big root;
// beyond this the dispatch loop waits, which is what it always did.
const maxConcurrentReads = 8

// scopeID is this session's COW scope, or nil for the baseline. Read through here
// rather than off the field: hello writes it on the request loop while reads run
// beside it.
func (s *Session) scopeID() *string { return s.scope.Load() }

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
	case req.Migration != nil:
		s.handleMigration(req.ID, req.Migration)
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
	// Store scope for this session (applies to all operations)
	s.scope.Store(req.Scope)
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
		s.usePending.Store(true)
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
