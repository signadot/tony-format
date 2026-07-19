package libctl

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// LogdSession manages a controller's session with logd.
//
// The logd session protocol is asynchronous and multiplexed: every request may
// carry an id that its response echoes, and the server also pushes unsolicited
// watch events (which carry no id). LogdSession models this directly. A single
// connection is shared by all operations; a read-pump goroutine demultiplexes
// incoming messages, routing responses to the request that is waiting on their
// id and routing events to the matching Watch by path. This lets many watches
// and in-flight requests share one connection.
type LogdSession struct {
	addr     string
	clientID string
	scope    *string // COW scope for this session; nil = baseline
	log      *slog.Logger

	mu        sync.Mutex
	conn      net.Conn
	connected bool
	serverID  string
	closed    bool
	readErr   error // last read-pump failure, surfaced to blocked callers

	nextID   uint64                               // request id counter
	pending  map[string]chan *api.SessionResponse // in-flight requests by id
	watchers map[string]*Watch                    // active watches by path

	// For shutdown
	done chan struct{}
}

// LogdSessionConfig contains configuration for connecting to logd.
type LogdSessionConfig struct {
	// Addr is the address of logd (e.g., "localhost:9091")
	Addr string

	// ClientID identifies this client to logd
	ClientID string

	// Scope selects a copy-on-write scope for the session. When non-empty, every
	// operation on the session (match, patch, watch, newtx) is isolated to this
	// scope: reads see the scope's data overlaid on baseline (COW), and writes
	// go to the scope without touching baseline. When empty, the session operates
	// on baseline, which is also the only kind of session that may DeleteScope or
	// modify the schema.
	Scope string

	// Log is an optional logger
	Log *slog.Logger
}

// NewLogdSession creates a new logd session.
// Call Connect to establish the connection.
func NewLogdSession(cfg *LogdSessionConfig) *LogdSession {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}

	var scope *string
	if cfg.Scope != "" {
		s := cfg.Scope
		scope = &s
	}

	return &LogdSession{
		addr:     cfg.Addr,
		clientID: cfg.ClientID,
		scope:    scope,
		log:      log.With("component", "logd-session"),
		pending:  make(map[string]chan *api.SessionResponse),
		watchers: make(map[string]*Watch),
		done:     make(chan struct{}),
	}
}

// Connect establishes connection to logd with retry.
func (s *LogdSession) Connect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.connected {
		return nil
	}

	return s.connectLocked(ctx)
}

// connectLocked establishes connection with retry (must hold mutex).
// On success it performs the hello handshake synchronously and then starts the
// read-pump goroutine, which owns the decoder from that point on.
func (s *LogdSession) connectLocked(ctx context.Context) error {
	backoff := 100 * time.Millisecond
	maxBackoff := 5 * time.Second

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.done:
			return fmt.Errorf("session closed")
		default:
		}

		conn, err := net.DialTimeout("tcp", s.addr, 5*time.Second)
		if err != nil {
			s.log.Debug("failed to connect to logd, retrying", "addr", s.addr, "error", err, "backoff", backoff)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-s.done:
				return fmt.Errorf("session closed")
			case <-time.After(backoff):
			}

			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		// Create decoder for responses
		decoder, err := stream.NewDecoder(conn, stream.WithBrackets())
		if err != nil {
			conn.Close()
			return fmt.Errorf("failed to create decoder: %w", err)
		}

		// Perform the hello handshake synchronously, before the read-pump
		// takes over the decoder.
		if err := s.sendHello(conn); err != nil {
			conn.Close()
			return fmt.Errorf("hello failed: %w", err)
		}
		resp, err := s.readResponseWith(decoder)
		if err != nil {
			conn.Close()
			return fmt.Errorf("failed to read hello response: %w", err)
		}
		if resp.Error != nil {
			conn.Close()
			return fmt.Errorf("hello error: %s", resp.Error.Message)
		}
		if resp.Result == nil || resp.Result.Hello == nil {
			conn.Close()
			return fmt.Errorf("unexpected response: no hello result")
		}

		s.conn = conn
		s.connected = true
		s.serverID = resp.Result.Hello.ServerID
		s.readErr = nil
		s.log.Info("connected to logd", "addr", s.addr, "serverID", s.serverID)

		go s.readPump(conn, decoder)
		return nil
	}
}

// sendHello sends the hello message to logd.
func (s *LogdSession) sendHello(conn net.Conn) error {
	req := &api.SessionRequest{
		Hello: &api.Hello{
			ClientID: s.clientID,
			Scope:    s.scope,
		},
	}
	return s.sendRequestTo(conn, req)
}

// ensureConnected checks connection and reconnects if needed (must hold mutex).
func (s *LogdSession) ensureConnected(ctx context.Context) error {
	if s.connected {
		return nil
	}
	return s.connectLocked(ctx)
}

// newIDLocked returns a fresh request id (must hold mutex).
func (s *LogdSession) newIDLocked() string {
	s.nextID++
	return strconv.FormatUint(s.nextID, 10)
}

// Match performs a match query at the given path.
func (s *LogdSession) Match(ctx context.Context, path string) (*ir.Node, error) {
	resp, err := s.request(ctx, &api.SessionRequest{
		Match: &api.MatchRequest{
			Body: api.PathData{Path: path},
		},
	})
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("match error: %s", resp.Error.Message)
	}
	if resp.Result == nil || resp.Result.Match == nil {
		return nil, fmt.Errorf("unexpected response: no match result")
	}
	return resp.Result.Match.Body, nil
}

// Patch applies a patch operation at the given path.
func (s *LogdSession) Patch(ctx context.Context, path string, data *ir.Node) error {
	resp, err := s.request(ctx, &api.SessionRequest{
		Patch: &api.PatchRequest{
			PathData: api.PathData{Path: path, Data: data},
		},
	})
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("patch error: %s", resp.Error.Message)
	}
	return nil
}

// NewTx creates a multi-participant transaction and returns its id. The
// transaction commits atomically once `participants` patches have joined it (by
// writing with the returned id via PatchTx). participants must be >= 1.
func (s *LogdSession) NewTx(ctx context.Context, participants int) (int64, error) {
	resp, err := s.request(ctx, &api.SessionRequest{
		NewTx: &api.NewTxRequest{Participants: participants},
	})
	if err != nil {
		return 0, err
	}
	if resp.Error != nil {
		return 0, fmt.Errorf("newtx error: %s", resp.Error.Message)
	}
	if resp.Result == nil || resp.Result.NewTx == nil {
		return 0, fmt.Errorf("unexpected response: no newtx result")
	}
	return resp.Result.NewTx.TxID, nil
}

// PatchTx applies a patch as a participant in the transaction txID. The call
// blocks until the transaction commits (all participants have joined) or fails.
// This is how a participant joins a transaction — the write is the join.
func (s *LogdSession) PatchTx(ctx context.Context, path string, data *ir.Node, txID int64) error {
	resp, err := s.request(ctx, &api.SessionRequest{
		Patch: &api.PatchRequest{
			TxID:     &txID,
			PathData: api.PathData{Path: path, Data: data},
		},
	})
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("patch error: %s", resp.Error.Message)
	}
	return nil
}

// DeleteScope deletes a copy-on-write scope and all of its data. It is only
// valid from a baseline session (one created with an empty Scope); logd rejects
// the request otherwise.
func (s *LogdSession) DeleteScope(ctx context.Context, scopeID string) error {
	resp, err := s.request(ctx, &api.SessionRequest{
		DeleteScope: &api.DeleteScopeRequest{
			ScopeID: scopeID,
		},
	})
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("deleteScope error: %s", resp.Error.Message)
	}
	return nil
}

// request sends a request and waits for its correlated response. It assigns a
// unique id, registers a reply channel that the read-pump delivers to, sends
// the request, and blocks until the response arrives, the context is cancelled,
// the session closes, or the connection fails.
func (s *LogdSession) request(ctx context.Context, req *api.SessionRequest) (*api.SessionResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, fmt.Errorf("session closed")
	default:
	}

	s.mu.Lock()
	if err := s.ensureConnected(ctx); err != nil {
		s.mu.Unlock()
		return nil, err
	}

	id := s.newIDLocked()
	req.ID = &id
	replyCh := make(chan *api.SessionResponse, 1)
	s.pending[id] = replyCh

	// Write while holding the mutex so concurrent requests don't interleave
	// bytes on the wire.
	if err := s.sendRequestTo(s.conn, req); err != nil {
		delete(s.pending, id)
		conn, pending, watchers := s.teardownLocked(err)
		s.mu.Unlock()
		releaseResources(conn, pending, watchers, err)
		return nil, err
	}
	s.mu.Unlock()

	select {
	case resp, ok := <-replyCh:
		if !ok {
			// Channel closed by teardown: the connection failed.
			return nil, s.connError()
		}
		return resp, nil
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return nil, ctx.Err()
	case <-s.done:
		return nil, fmt.Errorf("session closed")
	}
}

// readPump reads documents from the connection and demultiplexes them: events
// (no id) are routed to the matching Watch by path; everything else is a
// response routed to the request waiting on its id. It runs until the
// connection fails or is closed.
func (s *LogdSession) readPump(conn net.Conn, decoder *stream.Decoder) {
	for {
		node, err := readDocument(decoder)
		if err != nil {
			s.failConn(conn, err)
			return
		}

		var resp api.SessionResponse
		if err := resp.FromTonyIR(node); err != nil {
			s.failConn(conn, fmt.Errorf("failed to parse response: %w", err))
			return
		}

		if resp.Event != nil {
			s.routeEvent(resp.Event)
			continue
		}

		s.deliverResponse(&resp)
	}
}

// deliverResponse routes a response to the request waiting on its id.
func (s *LogdSession) deliverResponse(resp *api.SessionResponse) {
	s.mu.Lock()
	var ch chan *api.SessionResponse
	if resp.ID != nil {
		ch = s.pending[*resp.ID]
		delete(s.pending, *resp.ID)
	}
	s.mu.Unlock()

	if ch == nil {
		s.log.Warn("dropping response with no matching request", "id", resp.ID)
		return
	}
	ch <- resp // buffered (cap 1); never blocks
}

// routeEvent routes a watch event to the Watch registered for its path.
func (s *LogdSession) routeEvent(ev *api.WatchEvent) {
	s.mu.Lock()
	w := s.watchers[ev.Path]
	s.mu.Unlock()

	if w == nil {
		// The watch may have just been closed; drop the event.
		return
	}
	w.deliver(ev)
}

// failConn tears down the connection after a read-pump failure, but only if
// conn is still the session's current connection (guards against a stale pump
// from a previous connection).
func (s *LogdSession) failConn(conn net.Conn, err error) {
	s.mu.Lock()
	if s.conn != conn {
		s.mu.Unlock()
		return
	}
	c, pending, watchers := s.teardownLocked(err)
	s.mu.Unlock()
	releaseResources(c, pending, watchers, err)
}

// teardownLocked resets connection state and returns the resources that must be
// released after the mutex is dropped (the connection, the in-flight request
// channels, and the active watches). Must hold the mutex.
func (s *LogdSession) teardownLocked(err error) (net.Conn, map[string]chan *api.SessionResponse, map[string]*Watch) {
	conn := s.conn
	pending := s.pending
	watchers := s.watchers

	s.conn = nil
	s.connected = false
	s.pending = make(map[string]chan *api.SessionResponse)
	s.watchers = make(map[string]*Watch)
	if err != nil {
		s.readErr = err
	}
	return conn, pending, watchers
}

// releaseResources closes the connection, unblocks waiting requests (by closing
// their reply channels), and fails active watches. Called without the mutex.
func releaseResources(conn net.Conn, pending map[string]chan *api.SessionResponse, watchers map[string]*Watch, err error) {
	if conn != nil {
		conn.Close()
	}
	for _, ch := range pending {
		close(ch)
	}
	for _, w := range watchers {
		w.fail(err)
	}
}

// disconnect tears down the current connection. Callers must hold the mutex.
// Exposed for tests that simulate a connection break.
func (s *LogdSession) disconnect() {
	conn, pending, watchers := s.teardownLocked(fmt.Errorf("disconnected"))
	releaseResources(conn, pending, watchers, fmt.Errorf("disconnected"))
}

// connError reports why a blocked request was unblocked by a connection
// failure.
func (s *LogdSession) connError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readErr != nil {
		return fmt.Errorf("connection lost: %w", s.readErr)
	}
	return fmt.Errorf("connection lost")
}

// removeWatcher unregisters a watch by path.
func (s *LogdSession) removeWatcher(path string) {
	s.mu.Lock()
	delete(s.watchers, path)
	s.mu.Unlock()
}

// sendRequestTo sends a request to the given connection.
func (s *LogdSession) sendRequestTo(conn net.Conn, req *api.SessionRequest) error {
	data, err := req.ToTony(gomap.EncodeWire(true))
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write request: %w", err)
	}
	return nil
}

// readResponseWith reads a single response using the given decoder. Used only
// for the synchronous hello handshake; afterwards the read-pump owns reads.
func (s *LogdSession) readResponseWith(decoder *stream.Decoder) (*api.SessionResponse, error) {
	node, err := readDocument(decoder)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, fmt.Errorf("empty response")
	}

	var resp api.SessionResponse
	if err := resp.FromTonyIR(node); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &resp, nil
}

// readDocument reads events until we have a complete document.
func readDocument(decoder *stream.Decoder) (*ir.Node, error) {
	var events []stream.Event
	started := false

	for {
		event, err := decoder.ReadEvent()
		if err != nil {
			if err == io.EOF {
				if len(events) > 0 {
					return stream.EventsToNode(events)
				}
				return nil, io.EOF
			}
			return nil, err
		}

		events = append(events, *event)
		started = true

		if started && decoder.Depth() == 0 {
			return stream.EventsToNode(events)
		}
	}
}

// ServerID returns the logd server ID from the handshake.
func (s *LogdSession) ServerID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serverID
}

// Scope returns the session's copy-on-write scope, or "" for a baseline
// session.
func (s *LogdSession) Scope() string {
	if s.scope == nil {
		return ""
	}
	return *s.scope
}

// Connected returns whether the session is currently connected.
func (s *LogdSession) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}

// Close shuts down the session and closes the connection. Any active watches
// are failed and in-flight requests are unblocked.
func (s *LogdSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.done)
	conn, pending, watchers := s.teardownLocked(fmt.Errorf("session closed"))
	s.mu.Unlock()

	releaseResources(conn, pending, watchers, fmt.Errorf("session closed"))
	return nil
}
