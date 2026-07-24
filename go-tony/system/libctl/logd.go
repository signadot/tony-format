package libctl

import (
	"context"
	"errors"
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
	watchers map[string]*Watch                    // active watches by request id

	heartbeatInterval time.Duration // how often to ping; 0 disables the heartbeat
	heartbeatTimeout  time.Duration // how long to wait for a pong before tearing down
	wireTimeout       time.Duration // deadline for a single request write / hello handshake

	// For shutdown
	done chan struct{}
}

// Default session liveness settings. TCP keepalive catches a genuinely dead peer;
// the application heartbeat (ping/pong) catches a peer whose TCP is alive but whose
// session loop is gone (a wedged/half-open session), which keepalive cannot see.
const (
	defaultHeartbeatInterval = 10 * time.Second
	defaultHeartbeatTimeout  = 5 * time.Second
	tcpKeepAlivePeriod       = 15 * time.Second
	// defaultWireTimeout bounds a single request write and the hello handshake read. request()
	// holds s.mu across the write, so without a deadline a peer that stops READING (TCP alive,
	// send buffer full) blocks the write — and thus s.mu — forever, freezing every operation
	// including the heartbeat's own recovery ping, which needs s.mu (issue 9zkm8f1y). A
	// bounded write errors instead, triggering teardown + reconnect.
	defaultWireTimeout = 30 * time.Second
)

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

	// HeartbeatInterval is how often the session pings the server to prove the
	// connection is live. Zero uses a default; negative disables the heartbeat.
	HeartbeatInterval time.Duration

	// HeartbeatTimeout is how long a ping waits for its pong before the session is
	// declared wedged and torn down (failing pending requests so callers reconnect).
	// Zero uses a default.
	HeartbeatTimeout time.Duration

	// WireTimeout bounds a single request write and the hello handshake so a peer that
	// stops reading cannot block s.mu forever. Zero uses a default (30s).
	WireTimeout time.Duration
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

	interval := cfg.HeartbeatInterval
	if interval == 0 {
		interval = defaultHeartbeatInterval
	}
	timeout := cfg.HeartbeatTimeout
	if timeout == 0 {
		timeout = defaultHeartbeatTimeout
	}
	wireTimeout := cfg.WireTimeout
	if wireTimeout <= 0 {
		wireTimeout = defaultWireTimeout
	}

	return &LogdSession{
		addr:              cfg.Addr,
		clientID:          cfg.ClientID,
		scope:             scope,
		log:               log.With("component", "logd-session"),
		pending:           make(map[string]chan *api.SessionResponse),
		watchers:          make(map[string]*Watch),
		heartbeatInterval: interval,
		heartbeatTimeout:  timeout,
		wireTimeout:       wireTimeout,
		done:              make(chan struct{}),
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

		// TCP keepalive so the OS surfaces a genuinely dead peer as a read error
		// (the application heartbeat below covers the harder case: a peer whose TCP
		// is alive but whose session loop is gone).
		if tc, ok := conn.(*net.TCPConn); ok {
			_ = tc.SetKeepAlive(true)
			_ = tc.SetKeepAlivePeriod(tcpKeepAlivePeriod)
		}

		// Create decoder for responses
		decoder, err := stream.NewDecoder(conn, stream.WithBrackets())
		if err != nil {
			conn.Close()
			return fmt.Errorf("failed to create decoder: %w", err)
		}

		// Perform the hello handshake synchronously, before the read-pump takes over the
		// decoder. Bound the handshake read: a peer that completes the TCP dial but never
		// answers hello must not block forever under s.mu. Cleared before the read-pump starts
		// so the pump's long-lived reads aren't deadlined. (The hello write is bounded by
		// sendRequestTo's write deadline.)
		_ = conn.SetReadDeadline(time.Now().Add(s.wireTimeout))
		if err := s.sendHello(conn); err != nil {
			conn.Close()
			return fmt.Errorf("hello failed: %w", err)
		}
		resp, err := s.readResponseWith(decoder)
		if err != nil {
			conn.Close()
			return fmt.Errorf("failed to read hello response: %w", err)
		}
		_ = conn.SetReadDeadline(time.Time{}) // clear; the read-pump owns reads from here
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
		if s.heartbeatInterval > 0 {
			go s.heartbeat(conn)
		}
		return nil
	}
}

// heartbeat periodically pings the server on conn and, if a pong does not arrive
// within heartbeatTimeout, tears the connection down so the read-pump errors and
// every pending request fails (the caller then reconnects) — rather than hanging
// forever on a wedged or half-open session. It exits once conn is no longer the
// session's connection (a reconnect starts a fresh heartbeat) or the session closes.
func (s *LogdSession) heartbeat(conn net.Conn) {
	ticker := time.NewTicker(s.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
		}

		s.mu.Lock()
		current := s.conn == conn && s.connected
		s.mu.Unlock()
		if !current {
			return // conn replaced or session down; its own path handles teardown
		}

		ctx, cancel := context.WithTimeout(context.Background(), s.heartbeatTimeout)
		_, err := s.request(ctx, &api.SessionRequest{Ping: &api.PingRequest{}})
		cancel()
		if err == nil {
			continue
		}
		if errors.Is(err, context.DeadlineExceeded) {
			// No pong in time: the session is wedged. Closing conn unblocks the
			// read-pump, which fails all pending requests via teardown.
			s.log.Warn("session heartbeat timed out; tearing down connection", "addr", s.addr, "serverID", s.serverID)
			conn.Close()
		}
		return // any error means this connection is done; a new one gets a new heartbeat
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

// Match performs a match query at the given path, returning the full state
// there.
func (s *LogdSession) Match(ctx context.Context, path string) (*ir.Node, error) {
	return s.matchAt(ctx, path, nil, nil)
}

// MatchPattern performs a match query at path with a match/trim pattern: only the
// portion of the state matching pattern is returned, trimmed to the pattern's
// shape (field selection and array filtering). A nil pattern returns the full
// state, exactly like Match. The pattern rides the request as PathData.Data, so
// it applies whether the path is served by logd, a controller, or a docd-composed
// read across mounts.
func (s *LogdSession) MatchPattern(ctx context.Context, path string, pattern *ir.Node) (*ir.Node, error) {
	return s.matchAt(ctx, path, pattern, nil)
}

// MatchAt performs a point-in-time match query at path, returning the full state
// as of the given commit rather than the current one. The commit must be in range
// [0, current]; an out-of-range commit is rejected. Across docd this reads base
// and every logd-backed mount at the same commit — one consistent snapshot, since
// they share logd's single commit sequence.
func (s *LogdSession) MatchAt(ctx context.Context, path string, commit int64) (*ir.Node, error) {
	return s.matchAt(ctx, path, nil, &commit)
}

// MatchPatternAt combines MatchPattern and MatchAt: a point-in-time read at commit,
// trimmed to pattern.
func (s *LogdSession) MatchPatternAt(ctx context.Context, path string, pattern *ir.Node, commit int64) (*ir.Node, error) {
	return s.matchAt(ctx, path, pattern, &commit)
}

// matchAt is the general form behind Match/MatchPattern/MatchAt: a match at path
// with an optional trim pattern and an optional historical commit (nil = current).
func (s *LogdSession) matchAt(ctx context.Context, path string, pattern *ir.Node, commit *int64) (*ir.Node, error) {
	resp, err := s.request(ctx, &api.SessionRequest{
		Match: &api.MatchRequest{
			Body:   api.PathData{Path: path, Data: pattern},
			Commit: commit,
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

// ErrMatchFailed is returned by PatchIf/PatchTxIf when the compare-and-swap
// precondition did not hold against current state, so the patch was not applied.
// Callers doing optimistic concurrency can detect this with errors.Is and retry.
var ErrMatchFailed = errors.New("match precondition failed")

// doPatch sends a patch request and maps the response, surfacing a failed
// compare-and-swap precondition as ErrMatchFailed.
func (s *LogdSession) doPatch(ctx context.Context, req *api.PatchRequest) error {
	resp, err := s.request(ctx, &api.SessionRequest{Patch: req})
	if err != nil {
		return err
	}
	if resp.Error != nil {
		if resp.Error.Code == api.ErrCodeMatchFailed {
			return ErrMatchFailed
		}
		return fmt.Errorf("patch error: %s", resp.Error.Message)
	}
	return nil
}

// Patch applies a patch operation at the given path.
func (s *LogdSession) Patch(ctx context.Context, path string, data *ir.Node) error {
	return s.doPatch(ctx, &api.PatchRequest{
		PathData: api.PathData{Path: path, Data: data},
	})
}

// PatchIf applies a patch only if the compare-and-swap precondition holds: the
// current state at match.Path must match the pattern match.Data (evaluated
// atomically at commit). The match path may differ from the patch path. Returns
// ErrMatchFailed if the precondition does not hold.
func (s *LogdSession) PatchIf(ctx context.Context, path string, data *ir.Node, match *api.PathData) error {
	return s.doPatch(ctx, &api.PatchRequest{
		Match:    match,
		PathData: api.PathData{Path: path, Data: data},
	})
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
	return s.doPatch(ctx, &api.PatchRequest{
		TxID:     &txID,
		PathData: api.PathData{Path: path, Data: data},
	})
}

// PatchOpts carries the optional aspects of a patch: a transaction to join, a
// compare-and-swap precondition, and a per-participant timeout.
type PatchOpts struct {
	TxID    *int64        // join this multi-participant transaction
	Match   *api.PathData // compare-and-swap precondition
	Timeout *string       // per-participant wait timeout (e.g. "10s"); aborts a stalled tx
}

// PatchWith applies a patch with the given options. It is the general form behind
// Patch/PatchTx/PatchIf/PatchTxIf, and is what a controller uses to faithfully
// forward a docd-routed transaction participant (tx id, precondition, timeout) to
// logd.
func (s *LogdSession) PatchWith(ctx context.Context, path string, data *ir.Node, opts PatchOpts) error {
	return s.doPatch(ctx, &api.PatchRequest{
		TxID:     opts.TxID,
		Match:    opts.Match,
		Timeout:  opts.Timeout,
		PathData: api.PathData{Path: path, Data: data},
	})
}

// PatchTxIf is PatchTx with a compare-and-swap precondition (see PatchIf). The
// match is evaluated atomically with all other participants' matches at commit;
// returns ErrMatchFailed if it does not hold.
func (s *LogdSession) PatchTxIf(ctx context.Context, path string, data *ir.Node, txID int64, match *api.PathData) error {
	return s.doPatch(ctx, &api.PatchRequest{
		TxID:     &txID,
		Match:    match,
		PathData: api.PathData{Path: path, Data: data},
	})
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
			s.routeEvent(resp.ID, resp.Event)
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

// routeEvent routes a watch event to its Watch. Events carry the originating
// watch's request id on the response (id), which is the routing key — several
// watches (even on one path) are demuxed by it. An id-less event (an older server
// that does not stamp one) falls back to the single watch registered on ev.Path. A
// terminal event (Ended) fails the watch with a WatchEndedError and unregisters it,
// so the application can re-establish (docd sends this when a mount/unmount
// force-ends a watch or the owning controller crashes).
func (s *LogdSession) routeEvent(id *string, ev *api.WatchEvent) {
	s.mu.Lock()
	var w *Watch
	var key string
	if id != nil {
		key = *id
		w = s.watchers[key]
	} else {
		for k, cand := range s.watchers {
			if cand.path == ev.Path {
				w, key = cand, k
				break
			}
		}
	}
	if ev.Ended && w != nil {
		delete(s.watchers, key)
	}
	s.mu.Unlock()

	if w == nil {
		// The watch may have just been closed; drop the event.
		return
	}
	if ev.Ended {
		w.fail(&WatchEndedError{Path: ev.Path, Reason: ev.EndReason, Commit: ev.Commit})
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

// removeWatcher unregisters a watch by its request id.
func (s *LogdSession) removeWatcher(id string) {
	s.mu.Lock()
	delete(s.watchers, id)
	s.mu.Unlock()
}

// sendRequestTo sends a request to the given connection.
func (s *LogdSession) sendRequestTo(conn net.Conn, req *api.SessionRequest) error {
	data, err := req.ToTony(gomap.EncodeWire(true))
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}
	// Write-only deadline: request() holds s.mu across this write, so a peer that stopped
	// reading must not block it (and s.mu) forever. On timeout the write errors, which the
	// caller turns into teardown + reconnect. The read-pump's reads are unaffected.
	_ = conn.SetWriteDeadline(time.Now().Add(s.wireTimeout))
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
