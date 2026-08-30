package libctl

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

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
	// usePending is carried into every hello this session sends, including a
	// reconnect's: a session which was testing a pending schema must not silently come
	// back on the active one.
	usePending bool
	scope      *string // COW scope for this session; nil = baseline
	log        *slog.Logger

	// mu guards the session's state — the fields below, and the pending/watcher
	// maps. It is held only for as long as it takes to read or update them, never
	// across I/O: a Go mutex takes no context, so a caller queued on one spends its
	// budget waiting and then reports a deadline for an operation that never
	// happened. The two things that DO take time — connecting and writing — are
	// serialized by the channel semaphores below, which a caller can wait on with
	// its own deadline.
	// knownCommit is the high-water mark KnownCommit reports. Atomic rather than
	// under mu: it is written from the read pump for every answer that carries a
	// commit, and read by callers who are not holding anything.
	knownCommit atomic.Int64

	mu        sync.Mutex
	conn      net.Conn
	connected bool
	serverID  string
	closed    bool
	readErr   error // last read-pump failure, surfaced to blocked callers

	// connecting admits one connector at a time; wire admits one writer at a time,
	// so concurrent requests do not interleave bytes on the shared connection. Both
	// have capacity 1 and are taken with select on the caller's context.
	connecting chan struct{}
	wire       chan struct{}

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
	// dialTimeout bounds one dial attempt. The caller's context bounds the retry
	// loop around it, so this only decides how long a single unanswered SYN costs.
	dialTimeout = 5 * time.Second
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

	// UsePending makes this session read and write against the PENDING schema, when a
	// migration is in progress: what a caller does to test a schema before completing
	// the migration to it. The session is failed with migration_aborted if the
	// migration is abandoned under it, and refused at hello if there is none.
	UsePending bool

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
		usePending:        cfg.UsePending,
		scope:             scope,
		log:               log.With("component", "logd-session"),
		pending:           make(map[string]chan *api.SessionResponse),
		watchers:          make(map[string]*Watch),
		connecting:        make(chan struct{}, 1),
		wire:              make(chan struct{}, 1),
		heartbeatInterval: interval,
		heartbeatTimeout:  timeout,
		wireTimeout:       wireTimeout,
		done:              make(chan struct{}),
	}
}

// Connect establishes connection to logd with retry.
func (s *LogdSession) Connect(ctx context.Context) error {
	return s.ensureConnected(ctx)
}

// connect dials, performs the hello handshake, and publishes the connection, then
// starts the read-pump goroutine, which owns the decoder from that point on.
//
// The session mutex is NOT held across any of this — only to publish the result.
// Holding it meant a reconnect stood between every other caller and state they
// needed, for as long as the reconnect took, which is unbounded: connectLocked
// looped on dial-and-back-off and consulted ctx only BETWEEN attempts. Callers with
// their own budgets inherited that patience, and every one of them then reported
// `context deadline exceeded` on a request that never reached the wire.
//
// Only one connector runs at a time (the connecting semaphore, taken by
// ensureConnected). The rest wait on their own deadlines.
func (s *LogdSession) connect(ctx context.Context) error {
	backoff := 100 * time.Millisecond
	maxBackoff := 5 * time.Second
	dialer := &net.Dialer{Timeout: dialTimeout}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.done:
			return fmt.Errorf("session closed")
		default:
		}

		conn, err := dialer.DialContext(ctx, "tcp", s.addr)
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
		// decoder. Bound it by the wire timeout AND by what the caller is waiting for,
		// whichever comes first: a peer that completes the TCP dial and then says nothing —
		// a blackholed route, a rolled pod, a server too busy to answer — held a caller who
		// asked for ten seconds for thirty. Cleared before the read-pump starts so the
		// pump's long-lived reads aren't deadlined.
		deadline := s.wireDeadline(ctx)
		_ = conn.SetReadDeadline(deadline)

		// A caller can also be cancelled outright, and a blocking read notices that
		// no more than it notices a deadline it was not given. Close the connection
		// under it so the caller comes back when it asked to. The watcher stops the
		// moment the handshake is over: past that point the connection is the
		// session's, outliving the request that happened to establish it — and
		// callers cancel their contexts as a matter of course.
		handshake := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				conn.Close()
			case <-s.done:
				conn.Close()
			case <-handshake:
			}
		}()

		resp, err := s.hello(conn, decoder, deadline)
		close(handshake)
		if err != nil {
			conn.Close()
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr // the read was interrupted because the caller gave up
			}
			return err
		}
		_ = conn.SetReadDeadline(time.Time{}) // clear; the read-pump owns reads from here
		s.mu.Lock()
		if s.closed {
			// Close ran while this handshake was in flight; publishing now would
			// strand the connection and its read-pump on a dead session.
			s.mu.Unlock()
			conn.Close()
			return fmt.Errorf("session closed")
		}
		s.conn = conn
		s.connected = true
		s.serverID = resp.Result.Hello.ServerID
		s.readErr = nil
		serverID := s.serverID
		s.mu.Unlock()
		s.log.Info("connected to logd", "addr", s.addr, "serverID", serverID)

		go s.readPump(conn, decoder)
		if s.heartbeatInterval > 0 {
			go s.heartbeat(conn)
		}
		return nil
	}
}

// wireDeadline bounds one wire operation by the wire timeout and by what the caller
// is actually waiting for, whichever expires first. A caller cannot be made to wait
// past its own deadline for an answer it will not use.
func (s *LogdSession) wireDeadline(ctx context.Context) time.Time {
	deadline := time.Now().Add(s.wireTimeout)
	if cd, ok := ctx.Deadline(); ok && cd.Before(deadline) {
		return cd
	}
	return deadline
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

// hello performs the handshake on a freshly dialled connection, bounding both the
// write and the read by deadline, and returns the server's answer.
func (s *LogdSession) hello(conn net.Conn, decoder *stream.Decoder, deadline time.Time) (*api.SessionResponse, error) {
	req := &api.SessionRequest{
		Hello: &api.Hello{
			ClientID:   s.clientID,
			Protocol:   api.ProtocolVersion,
			UsePending: s.usePending,
			Scope:      s.scope,
		},
	}
	if err := s.sendRequestWithin(conn, req, deadline); err != nil {
		return nil, fmt.Errorf("hello failed: %w", err)
	}
	resp, err := s.readResponseWith(decoder)
	if err != nil {
		return nil, fmt.Errorf("failed to read hello response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("hello error: %w", resp.Error)
	}
	if resp.Result == nil || resp.Result.Hello == nil {
		return nil, fmt.Errorf("unexpected response: no hello result")
	}
	// The server checks this too and refuses, so this is for the case where it CANNOT:
	// a server old enough not to know the field answers happily and then ignores whatever
	// this version says differently. Refusing here turns that into a message instead of a
	// read of the root (api.ProtocolVersion).
	if p := resp.Result.Hello.Protocol; p != 0 && p != api.ProtocolVersion {
		return nil, fmt.Errorf("%w: server %q speaks session protocol %d, this client speaks %d: deploy them together",
			ErrProtocolMismatch, resp.Result.Hello.ServerID, p, api.ProtocolVersion)
	}
	if resp.Result.Hello.Protocol == 0 {
		s.log.Warn("server does not report a session protocol version; it predates the check",
			"addr", s.addr, "serverID", resp.Result.Hello.ServerID, "client", api.ProtocolVersion)
	}
	return resp, nil
}

// ErrProtocolMismatch is returned by Connect when the server speaks a session protocol this
// client does not. It is not retryable: the deployment is wrong, not the moment.
var ErrProtocolMismatch = errors.New("session protocol mismatch")

// ensureConnected connects if the session is not connected. It must NOT be called
// with the session mutex held: one connector runs at a time, and the callers waiting
// behind it wait on their own contexts rather than on a lock that takes none.
func (s *LogdSession) ensureConnected(ctx context.Context) error {
	if s.Connected() {
		return nil
	}

	select {
	case s.connecting <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return fmt.Errorf("session closed")
	}
	defer func() { <-s.connecting }()

	// Someone else may have connected while we waited our turn.
	if s.Connected() {
		return nil
	}
	return s.connect(ctx)
}

// acquireWire takes the connection for one write, or gives up on the caller's terms.
// releaseWire hands it to the next writer.
func (s *LogdSession) acquireWire(ctx context.Context) error {
	select {
	case s.wire <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return fmt.Errorf("session closed")
	}
}

func (s *LogdSession) releaseWire() { <-s.wire }

// newIDLocked returns a fresh request id (must hold mutex).
func (s *LogdSession) newIDLocked() string {
	s.nextID++
	return strconv.FormatUint(s.nextID, 10)
}

// Match performs a match query at the given path, returning the full state
// there.
// Ping asks whether the session is alive, and answers with where the store is: the head
// commit and the oldest commit a watch may still replay from. Both come from memory --
// no read, no path -- which is what makes this the right question to ask when the
// question is about the connection.
//
// It was internal, used only by the heartbeat, so a caller wanting to know whether its
// session worked reached for a read of the root instead. That is a different question,
// and since a store where nothing has been written has nothing at any path, it is one
// with an answer that looks like a failure (bymhrqz7h12ksas3jhn0).
//
// Commit is zero when the answering server tracks no single head, which a router in
// front of several stores does not.
func (s *LogdSession) Ping(ctx context.Context) (*api.PongResult, error) {
	resp, err := s.request(ctx, &api.SessionRequest{Ping: &api.PingRequest{}})
	if err != nil {
		return nil, err
	}
	if resp.Result == nil || resp.Result.Pong == nil {
		return &api.PongResult{}, nil
	}
	return resp.Result.Pong, nil
}

func (s *LogdSession) Match(ctx context.Context, path string) (*ir.Node, error) {
	node, _, err := s.matchAt(ctx, path, nil, nil)
	return node, err
}

// MatchCommit is Match, and also the commit the read was taken at -- which every
// match answer has always carried and this package used to drop. A caller tracking
// where the store is gets it from the reads it already makes, rather than by opening
// a watch for its initial state (7qayp3hah12kscx2gdn0). See also KnownCommit, which
// is this, remembered, and kept current by the heartbeat when nothing is being read.
func (s *LogdSession) MatchCommit(ctx context.Context, path string) (*ir.Node, int64, error) {
	return s.matchAt(ctx, path, nil, nil)
}

// MatchPatternCommit is MatchPattern with the read's commit, as MatchCommit is to
// Match.
func (s *LogdSession) MatchPatternCommit(ctx context.Context, path string, pattern *ir.Node) (*ir.Node, int64, error) {
	return s.matchAt(ctx, path, pattern, nil)
}

// MatchPattern performs a match query at path with a match/trim pattern: only the
// portion of the state matching pattern is returned, trimmed to the pattern's
// shape (field selection and array filtering). A nil pattern returns the full
// state, exactly like Match. The pattern rides the request as PathData.Data, so
// it applies whether the path is served by logd, a controller, or a docd-composed
// read across mounts.
func (s *LogdSession) MatchPattern(ctx context.Context, path string, pattern *ir.Node) (*ir.Node, error) {
	node, _, err := s.matchAt(ctx, path, pattern, nil)
	return node, err
}

// MatchAt performs a point-in-time match query at path, returning the full state
// as of the given commit rather than the current one. The commit must be in range
// [0, current]; an out-of-range commit is rejected. Across docd this reads base
// and every logd-backed mount at the same commit — one consistent snapshot, since
// they share logd's single commit sequence.
func (s *LogdSession) MatchAt(ctx context.Context, path string, commit int64) (*ir.Node, error) {
	node, _, err := s.matchAt(ctx, path, nil, &commit)
	return node, err
}

// MatchPatternAt combines MatchPattern and MatchAt: a point-in-time read at commit,
// trimmed to pattern.
func (s *LogdSession) MatchPatternAt(ctx context.Context, path string, pattern *ir.Node, commit int64) (*ir.Node, error) {
	node, _, err := s.matchAt(ctx, path, pattern, &commit)
	return node, err
}

// matchAt is the general form behind Match/MatchPattern/MatchAt: a match at path
// with an optional trim pattern and an optional historical commit (nil = current).
func (s *LogdSession) matchAt(ctx context.Context, path string, pattern *ir.Node, commit *int64) (*ir.Node, int64, error) {
	resp, err := s.request(ctx, &api.SessionRequest{
		Match: &api.MatchRequest{
			PathData: api.PathData{Path: path, Data: pattern},
			Commit:   commit,
		},
	})
	if err != nil {
		return nil, 0, err
	}
	if resp.Error != nil {
		return nil, 0, fmt.Errorf("match error: %w", resp.Error)
	}
	if resp.Result == nil || resp.Result.Match == nil {
		return nil, 0, fmt.Errorf("unexpected response: no match result")
	}
	return resp.Result.Match.Body, resp.Result.Match.Commit, nil
}

// ErrMatchFailed is returned by PatchIf/PatchTxIf when the compare-and-swap
// precondition did not hold against current state, so the patch was not applied.
// Callers doing optimistic concurrency can detect this with errors.Is and retry.
//
// It predates the general code propagation (see the package doc) and is kept
// because callers use it. A match_failed response satisfies both
// errors.Is(err, ErrMatchFailed) and api.ErrorCode(err) == api.ErrCodeMatchFailed.
var ErrMatchFailed = errors.New("match precondition failed")

// doPatch sends a patch request and maps the response, surfacing a failed
// compare-and-swap precondition as ErrMatchFailed. On success it returns what
// the write landed as: api.PatchResult.Commit is the commit the patch committed
// at, and Data is the patched data as stored, with any auto-generated ids filled
// in. Data may be nil for a write docd split across mounts — there is no single
// stored subtree to return — but the commit is the transaction's, so it is always
// reported.
func (s *LogdSession) doPatch(ctx context.Context, req *api.PatchRequest) (*api.PatchResult, error) {
	resp, err := s.request(ctx, &api.SessionRequest{Patch: req})
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		if resp.Error.Code == api.ErrCodeMatchFailed {
			// Both wrapped: the sentinel callers already match on, and the response
			// itself, so this arm reports the server's message like every other arm
			// instead of replacing it with the sentinel's fixed text.
			return nil, fmt.Errorf("%w: %w", ErrMatchFailed, resp.Error)
		}
		return nil, fmt.Errorf("patch error: %w", resp.Error)
	}
	if resp.Result == nil || resp.Result.Patch == nil {
		return nil, fmt.Errorf("unexpected response: no patch result")
	}
	return resp.Result.Patch, nil
}

// Patch applies a patch operation at the given path. It returns the commit the
// write landed at and the data as stored (see doPatch).
func (s *LogdSession) Patch(ctx context.Context, path string, data *ir.Node) (*api.PatchResult, error) {
	return s.doPatch(ctx, &api.PatchRequest{
		PathData: api.PathData{Path: path, Data: data},
	})
}

// PatchIf applies a patch only if the compare-and-swap precondition holds: the
// current state at match.Path must match the pattern match.Data (evaluated
// atomically at commit). The match path may differ from the patch path. Returns
// ErrMatchFailed if the precondition does not hold. On success it returns the
// commit the write landed at and the data as stored (see doPatch).
func (s *LogdSession) PatchIf(ctx context.Context, path string, data *ir.Node, match *api.PathData) (*api.PatchResult, error) {
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
		return 0, fmt.Errorf("newtx error: %w", resp.Error)
	}
	if resp.Result == nil || resp.Result.NewTx == nil {
		return 0, fmt.Errorf("unexpected response: no newtx result")
	}
	return resp.Result.NewTx.TxID, nil
}

// PatchTx applies a patch as a participant in the transaction txID. The call
// blocks until the transaction commits (all participants have joined) or fails.
// This is how a participant joins a transaction — the write is the join. On
// success it returns the transaction's commit and the data as stored (see
// doPatch); every participant sees the same commit.
func (s *LogdSession) PatchTx(ctx context.Context, path string, data *ir.Node, txID int64) (*api.PatchResult, error) {
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
// logd. On success it returns the commit the write landed at and the data as
// stored (see doPatch) — which is what a controller hands back to docd so a
// controller-served write reports a commit like a direct logd write does.
func (s *LogdSession) PatchWith(ctx context.Context, path string, data *ir.Node, opts PatchOpts) (*api.PatchResult, error) {
	return s.doPatch(ctx, &api.PatchRequest{
		TxID:     opts.TxID,
		Match:    opts.Match,
		Timeout:  opts.Timeout,
		PathData: api.PathData{Path: path, Data: data},
	})
}

// PatchTxIf is PatchTx with a compare-and-swap precondition (see PatchIf). The
// match is evaluated atomically with all other participants' matches at commit;
// returns ErrMatchFailed if it does not hold. On success it returns the
// transaction's commit and the data as stored (see doPatch).
func (s *LogdSession) PatchTxIf(ctx context.Context, path string, data *ir.Node, txID int64, match *api.PathData) (*api.PatchResult, error) {
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
		return fmt.Errorf("deleteScope error: %w", resp.Error)
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

	if err := s.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Take the wire first: the id is registered only once this request is the one
	// being written, so a caller that gives up waiting its turn leaves nothing behind.
	if err := s.acquireWire(ctx); err != nil {
		return nil, err
	}

	s.mu.Lock()
	conn := s.conn
	if conn == nil {
		s.mu.Unlock()
		s.releaseWire()
		return nil, s.connError()
	}
	id := s.newIDLocked()
	req.ID = &id
	replyCh := make(chan *api.SessionResponse, 1)
	s.pending[id] = replyCh
	s.mu.Unlock()

	// Write with the wire held so concurrent requests don't interleave bytes.
	err := s.sendRequestTo(conn, req)
	s.releaseWire()
	if err != nil {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		s.failConn(conn, err)
		return nil, err
	}

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
		node, err := stream.ReadDocument(decoder)
		if err != nil {
			s.failConn(conn, err)
			return
		}

		var resp api.SessionResponse
		if err := resp.FromTonyIR(node); err != nil {
			s.failConn(conn, fmt.Errorf("failed to parse response: %w", err))
			return
		}

		// Everything the server says passes here, which is where this session
		// learns where the store has got to. See KnownCommit.
		s.noteResponseCommit(&resp)

		if resp.Event != nil {
			s.routeEvent(resp.ID, resp.Event)
			continue
		}

		s.deliverResponse(&resp)
	}
}

// KnownCommit is the highest commit this session has been told about, over every
// answer it has had: reads, writes, watch events, and the heartbeat's pong. It is
// monotonic and it chases the store's head without a watch, a poll or a read of its
// own -- the heartbeat keeps it current while the session is idle, and reads keep it
// current while it is busy.
//
// It says where the store has got to, not that this client has seen everything below
// it. Zero means nothing has said yet. Against docd it is docd's own mark over what it
// has told any client, which is a lower bound on the head rather than the head itself
// (7qayp3hah12kscx2gdn0).
func (s *LogdSession) KnownCommit() int64 { return s.knownCommit.Load() }

// noteCommit raises the mark KnownCommit reports.
func (s *LogdSession) noteCommit(commit int64) {
	for {
		cur := s.knownCommit.Load()
		if commit <= cur || s.knownCommit.CompareAndSwap(cur, commit) {
			return
		}
	}
}

// noteResponseCommit takes whatever a response says about where the store is. Every
// answer from the server passes here, so a caller which never asks still learns from
// the heartbeat.
func (s *LogdSession) noteResponseCommit(resp *api.SessionResponse) {
	if ev := resp.Event; ev != nil {
		s.noteCommit(ev.Commit)
	}
	r := resp.Result
	if r == nil {
		return
	}
	if r.Match != nil {
		s.noteCommit(r.Match.Commit)
	}
	if r.Patch != nil {
		s.noteCommit(r.Patch.Commit)
	}
	if r.Pong != nil {
		s.noteCommit(r.Pong.Commit)
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
		// Log the id, not the pointer to it: this line is read when a response and
		// its request have parted company, which is a question about which id.
		id := "<none>"
		if resp.ID != nil {
			id = *resp.ID
		}
		s.log.Warn("dropping response with no matching request", "id", id)
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

// sendRequestTo sends a request to the given connection, bounded by the wire timeout.
func (s *LogdSession) sendRequestTo(conn net.Conn, req *api.SessionRequest) error {
	return s.sendRequestWithin(conn, req, time.Now().Add(s.wireTimeout))
}

// sendRequestWithin sends a request to the given connection, bounded by deadline.
//
// The write is deadlined because the writer holds the wire while it runs: a peer that
// stopped READING (TCP alive, send buffer full) would otherwise block every other
// writer, including the heartbeat's own recovery ping (issue 9zkm8f1y). On timeout
// the write errors, which the caller turns into teardown + reconnect. The read-pump's
// reads are unaffected.
func (s *LogdSession) sendRequestWithin(conn net.Conn, req *api.SessionRequest, deadline time.Time) error {
	data, err := req.ToTony(api.WireOptions()...)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}
	_ = conn.SetWriteDeadline(deadline)
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write request: %w", err)
	}
	return nil
}

// readResponseWith reads a single response using the given decoder. Used only
// for the synchronous hello handshake; afterwards the read-pump owns reads.
func (s *LogdSession) readResponseWith(decoder *stream.Decoder) (*api.SessionResponse, error) {
	node, err := stream.ReadDocument(decoder)
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
