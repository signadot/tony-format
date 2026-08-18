package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// ClientSession serves a client speaking the logd session protocol and routes
// each operation to its owner:
//
//   - operations under a mounted subtree go to the owning controller (via that
//     controller's MountSession), which answers for its subtree;
//   - everything else — base/unmounted paths, the hello handshake, and
//     session-level operations (newtx, schema, deleteScope) — goes straight to
//     logd over a per-client logd connection.
//
// Responses flow back from two sources — the logd read-pump and controller
// MountSessions — so writes to the client connection are serialized. Because
// each client has its own logd connection, base-path traffic needs no id
// rewriting; only the shared controller connections do (handled in
// MountSession).
type ClientSession struct {
	id       string
	conn     net.Conn
	server   *Server
	log      *slog.Logger
	logdAddr string

	clientDec *stream.Decoder

	// clientScope is the COW scope from the client's hello, if any. Only the
	// request loop touches it. Baseline (nil) NewTx is served from docd's pool;
	// a scoped NewTx is forwarded to logd on the client's scoped connection.
	clientScope *string

	logd    net.Conn
	logdDec *stream.Decoder
	logdWMu sync.Mutex // serializes writes to logd (request loop + watch coordination + force teardown)

	writeMu sync.Mutex // serializes writes to the client connection

	// watchMu guards the active-watch bookkeeping. Each active watch holds a
	// coordinator reader token so a mount/unmount can drain or force-end the watches
	// that overlap it. watches is keyed by the watch KEY (the client's request id, or
	// the path for a legacy id-less watch), so a session can hold several watches on
	// the same path. closing is set during teardown so a still-registering watch
	// releases its token instead of leaking it past the session.
	watchMu sync.Mutex
	watches map[string]*clientWatch
	// clockWatches holds live docd-driven clock watches (see clock.go), keyed by the
	// same watch key as watches. Clocks are served directly by docd (no controller,
	// no coordinator token), so they are tracked separately. Guarded by watchMu.
	clockWatches map[string]*clockWatcher
	closing      bool

	// lastSeenMu guards lastSeen: the highest commit delivered to the client per
	// watch (keyed by watch key). A force-end stamps it onto the terminal WatchEvent
	// so the client can re-watch FromCommit and resume with no gap. Exact for
	// single-route watches; best-effort for composed watches, whose sub-streams have
	// independent commit sequences, until composed watches honor FromCommit on
	// re-establish.
	lastSeenMu sync.Mutex
	lastSeen   map[string]int64

	mountsMu   sync.Mutex
	usedMounts map[*MountSession]struct{} // mounts this client routed to, for teardown

	done      chan struct{} // closed on Close, to abort the logd dial-retry
	closeOnce sync.Once

	writeTimeout time.Duration // per-write deadline to the client (see clientWriteTimeout)
}

// logdConnectTimeout bounds how long a client session retries dialing logd
// before giving up (and failing the client, which reconnects). Generous enough
// to cover startup ordering and brief logd unavailability.
const logdConnectTimeout = 30 * time.Second

// clientWriteTimeout bounds a single write to the client connection. Without it, a slow or
// dead client (a full TCP send buffer) blocks writeToClient forever — and because the mount
// coordinator's force path and composed-watch forwarding call it synchronously, one stuck
// client wedges watch/mount coordination for its whole path region (issue 0tarechx).
// Generous, so a legitimately slow-but-alive client isn't failed on normal responses.
const clientWriteTimeout = 30 * time.Second

// ClientSessionConfig contains configuration for creating a client session.
type ClientSessionConfig struct {
	Log          *slog.Logger
	Server       *Server
	WriteTimeout time.Duration // per-write deadline to the client (default: clientWriteTimeout)
}

// NewClientSession creates a new client-facing session for the given connection.
func NewClientSession(id string, conn net.Conn, cfg *ClientSessionConfig) *ClientSession {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	wt := cfg.WriteTimeout
	if wt <= 0 {
		wt = clientWriteTimeout
	}
	return &ClientSession{
		id:           id,
		conn:         conn,
		server:       cfg.Server,
		log:          log.With("session", id),
		logdAddr:     cfg.Server.Spec.LogdAddr,
		watches:      make(map[string]*clientWatch),
		clockWatches: make(map[string]*clockWatcher),
		lastSeen:     make(map[string]int64),
		usedMounts:   make(map[*MountSession]struct{}),
		done:         make(chan struct{}),
		writeTimeout: wt,
	}
}

// Run dials logd, then pumps in both directions — client requests routed out to
// controllers or logd, and logd/controller responses back to the client — until
// either side closes. It blocks until the session ends.
func (s *ClientSession) Run() error {
	defer s.conn.Close()

	clientDec, err := stream.NewDecoder(s.conn, stream.WithBrackets())
	if err != nil {
		return fmt.Errorf("failed to create client decoder: %w", err)
	}
	s.clientDec = clientDec

	logdConn, err := s.connectLogd()
	if err != nil {
		return err
	}
	defer logdConn.Close()
	s.logd = logdConn

	logdDec, err := stream.NewDecoder(logdConn, stream.WithBrackets())
	if err != nil {
		return fmt.Errorf("failed to create logd decoder: %w", err)
	}
	s.logdDec = logdDec

	s.log.Debug("client session started", "logd", s.logdAddr)

	// Two directions. Whichever finishes first tears down both connections,
	// unblocking the other pump; then routed watches are cleaned up.
	errc := make(chan error, 2)
	go func() { errc <- s.pumpLogdToClient() }()
	go func() { errc <- s.routeClientRequests() }()

	firstErr := <-errc
	s.conn.Close()
	logdConn.Close()
	<-errc

	s.releaseAllWatches()
	s.stopAllClockWatches()
	s.cleanupMounts()
	return firstErr
}

// routeClientRequests reads requests from the client and routes each to the
// owning controller or, for base/session operations, to logd.
func (s *ClientSession) routeClientRequests() error {
	for {
		node, err := stream.ReadDocument(s.clientDec)
		if err != nil {
			return ignoreClosed(err)
		}

		var req logdapi.SessionRequest
		if err := req.FromTonyIR(node); err != nil {
			_ = s.writeToClient(logdapi.NewErrorResponse(nil, logdapi.ErrCodeInvalidMessage,
				fmt.Sprintf("invalid request: %v", err)))
			continue
		}

		if req.Hello != nil {
			s.clientScope = req.Hello.Scope // remember for tx routing; still forwarded below
		}
		// Answer a liveness ping from docd itself: a Pong confirms this client
		// session's request loop is alive, which is exactly what a wedged-session
		// probe needs to detect. Do not forward it downstream.
		if req.Ping != nil {
			if err := s.writeToClient(logdapi.NewPongResponse(req.ID, s.server.seen.Load())); err != nil {
				return err
			}
			continue
		}
		// Serve a baseline NewTx from docd's pre-fetched pool (fewer hops). A
		// scoped NewTx falls through to logd on the client's scoped connection,
		// since pooled ids are baseline-scoped.
		if req.NewTx != nil && s.clientScope == nil {
			if err := s.serveNewTx(&req); err != nil {
				return err
			}
			continue
		}

		// docd-driven virtual clocks are served directly, like .meta: a clock has no
		// controller and needs no mount coordination, so intercept its reads and
		// watches before the mount-coordination paths below. Clocks are read-only.
		if clk := s.clockFor(&req); clk != nil {
			switch {
			case req.Match != nil:
				s.serveClockMatch(&req, clk)
			case req.Watch != nil:
				s.serveClockWatch(&req, clk)
			case req.Unwatch != nil:
				s.stopClockWatch(watchKeyFor(req.Unwatch.WatchID, req.Unwatch.Path))
			default:
				_ = s.writeToClient(logdapi.NewErrorResponse(req.ID, logdapi.ErrCodeUnsupported,
					"clock paths are read-only"))
			}
			continue
		}

		// A patch that spans multiple mounts is decomposed and committed as one
		// atomic transaction — in the client's scope when it has one (the tx id and
		// every participant are scoped). Single-participant patches fall through to
		// normal routing.
		if req.Patch != nil {
			handled, err := s.maybeCoordinatePatch(&req)
			if err != nil {
				return err
			}
			if handled {
				continue
			}
		}

		// A read whose path is a strict ancestor of one or more mounts is composed
		// from the base owner plus each mounted subtree, since docd single-routes and
		// would otherwise miss the mounts. Reads with no mount beneath them fall
		// through to normal single-route routing.
		if req.Match != nil && s.maybeCoordinateMatch(&req) {
			continue
		}

		// A watch registers as a reader with the mount coordinator so a concurrent
		// mount/unmount on an overlapping path drains or force-ends it. Coordination
		// runs off the request loop: a pending overlapping writer would otherwise
		// block the loop (and all this client's other ops) until the mount resolves.
		if req.Watch != nil {
			s.coordinateWatch(&req)
			continue
		}
		// Releasing the reader token before routing the unwatch keeps a concurrent
		// mount from waiting on a watch the client has already dropped. The unwatch
		// targets a specific watch by its id (WatchID); without one it drops the
		// legacy path-keyed watch.
		if req.Unwatch != nil {
			s.releaseWatchToken(watchKeyFor(req.Unwatch.WatchID, req.Unwatch.Path))
		}

		switch dest, entry := s.routeFor(&req); dest {
		case destController:
			entry.Session.RouteRequest(s, &req)
		case destUnavailable:
			_ = s.writeToClient(logdapi.NewErrorResponse(req.ID, logdapi.ErrCodeUnavailable,
				fmt.Sprintf("controller for %q is unavailable", entry.Path)))
		case destMeta:
			s.serveMeta(&req)
		default: // destLogd
			if err := s.writeToLogd(&req); err != nil {
				return err
			}
		}
	}
}

// routeDest classifies where a request should go.
type routeDest int

const (
	destLogd        routeDest = iota // base/unmounted path or session op -> logd
	destController                   // mounted subtree with a live controller
	destUnavailable                  // mounted subtree whose controller has crashed (tombstone)
	destMeta                         // docd's own .meta subtree (mounts, schema, ...)
)

// serveMeta answers a request on docd's reserved .meta subtree from docd's own
// state. Metadata is read-only, so only MATCH is served; an unknown resource
// returns a null body, mirroring logd's match-on-missing-path behavior.
func (s *ClientSession) serveMeta(req *logdapi.SessionRequest) {
	if req.Match == nil {
		_ = s.writeToClient(logdapi.NewErrorResponse(req.ID, logdapi.ErrCodeUnsupported,
			"metadata paths are read-only"))
		return
	}

	var body *ir.Node
	switch metaLeaf(req.Match.Body.Path) {
	case "":
		body = metaIndexDoc()
	case "clocks":
		body = clocksDoc(s.server.Clocks.list())
	case "mounts":
		body = mountsDoc(s.server.Mounts.List())
	case "schema":
		body = schemaDoc(s.server.Mounts.List())
	default:
		body = ir.Null()
	}
	_ = s.writeToClient(&logdapi.SessionResponse{
		ID:     req.ID,
		Result: &logdapi.SessionResult{Match: &logdapi.MatchResult{Body: body}},
	})
}

// pumpLogdToClient forwards logd's responses and watch events back to the
// client. Serialized against controller responses via writeToClient.
func (s *ClientSession) pumpLogdToClient() error {
	for {
		node, err := stream.ReadDocument(s.logdDec)
		if err != nil {
			return ignoreClosed(err)
		}

		var resp logdapi.SessionResponse
		if err := resp.FromTonyIR(node); err != nil {
			s.log.Error("failed to parse logd response", "error", err)
			continue
		}
		if err := s.writeToClient(&resp); err != nil {
			return err
		}
	}
}

// txParticipantTimeout bounds how long each participant of a coordinated
// multi-mount transaction waits for the others; if a participant fails to write,
// the rest time out and the transaction aborts rather than hanging.
const txParticipantTimeout = 10 * time.Second

// maybeCoordinatePatch splits a client patch across mounts. If it spans two or
// more participants it is committed as one atomic transaction (handled here,
// returning handled=true) and the coordination runs in the background so the read
// loop keeps serving. A single-participant patch returns handled=false to fall
// through to normal routing. A patch that cannot be decomposed statically (a
// higher-order op above a mount boundary) is answered with an error.
func (s *ClientSession) maybeCoordinatePatch(req *logdapi.SessionRequest) (bool, error) {
	parts, base, err := splitPatch(s.server.Mounts, req.Patch.Path, req.Patch.Data, s.server.patchTagFilter())
	if err != nil {
		return true, s.writeToClient(logdapi.NewErrorResponse(req.ID, logdapi.ErrCodeInvalidMessage, err.Error()))
	}

	if len(parts)+len(base) < 2 {
		return false, nil // single participant: route normally
	}

	go s.coordinatePatch(req, parts, base)
	return true, nil
}

// allocTx allocates a transaction id for count participants. Baseline patches use
// docd's pre-fetched (scopeless) pool; a scoped client's transaction is created
// in its scope directly on logd, since the pool cannot serve scoped ids.
func (s *ClientSession) allocTx(count int, scope *string) (int64, error) {
	if scope != nil {
		return allocScopedTx(s.logdAddr, scope, count, 5*time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.server.txPool.Get(ctx, count)
}

// coordinatePatch commits a multi-mount patch as one transaction: it allocates a
// tx id for all participants (pooled for baseline, scoped otherwise), writes each
// mount's sub-patch to its
// controller and the base remainder over docd's own logd link (concurrently,
// since each blocks until the whole tx commits), then returns a single result to
// the client. The client's compare-and-swap precondition, if any, rides on one
// participant unsplit (the base if present, else the first mount).
func (s *ClientSession) coordinatePatch(req *logdapi.SessionRequest, parts []mountPart, base []baseWrite) {
	clientID := req.ID
	count := len(parts) + len(base)
	scope := s.clientScope

	txID, err := s.allocTx(count, scope)
	if err != nil {
		_ = s.writeToClient(logdapi.NewErrorResponse(clientID, logdapi.ErrCodeInvalidTx,
			fmt.Sprintf("failed to allocate transaction: %v", err)))
		return
	}

	ts := txParticipantTimeout.String()
	// Each participant's response is tagged with the path it wrote at, so the
	// data they report can be reassembled into the one subtree the client asked
	// for (joinPatchResults).
	type partResponse struct {
		path string
		resp *logdapi.SessionResponse
	}
	results := make(chan partResponse, count)

	// The client's precondition rides on exactly one participant, unsplit: the
	// first base write if any, else the first mount part.
	baseCarriesMatch := len(base) > 0

	for i, bw := range base {
		var matchNode *ir.Node
		var matchPath string
		if i == 0 && req.Patch.Match != nil {
			matchNode, matchPath = req.Patch.Match.Data, req.Patch.Match.Path
		}
		go func(bw baseWrite, matchNode *ir.Node, matchPath string) {
			resp, err := writeBaseParticipant(s.logdAddr, txID, bw.path, bw.data, matchNode, matchPath, scope, txParticipantTimeout)
			if err != nil {
				results <- partResponse{bw.path, logdapi.NewErrorResponse(nil, logdapi.ErrCodeSessionClosed, err.Error())}
				return
			}
			results <- partResponse{bw.path, resp}
		}(bw, matchNode, matchPath)
	}

	for i, p := range parts {
		var match *logdapi.PathData
		if !baseCarriesMatch && i == 0 {
			match = req.Patch.Match
		}
		preq := &logdapi.SessionRequest{
			Scope: scope,
			Patch: &logdapi.PatchRequest{
				TxID:     &txID,
				Timeout:  &ts,
				Match:    match,
				PathData: logdapi.PathData{Path: p.mount.Path, Data: p.data},
			},
		}
		ch := p.mount.Session.RouteCollect(preq)
		go func(path string, ch <-chan *logdapi.SessionResponse) {
			results <- partResponse{path, <-ch}
		}(p.mount.Path, ch)
	}

	var commit int64
	var firstErr *logdapi.SessionError
	reported := make([]participantResult, 0, count)
	for i := 0; i < count; i++ {
		pr := <-results
		switch {
		case pr.resp.Error != nil:
			if firstErr == nil {
				firstErr = pr.resp.Error
			}
		case pr.resp.Result != nil && pr.resp.Result.Patch != nil:
			// Every participant joined the same transaction, so they all report
			// its commit.
			commit = pr.resp.Result.Patch.Commit
			reported = append(reported, participantResult{path: pr.path, data: pr.resp.Result.Patch.Data})
		}
	}

	if firstErr != nil {
		_ = s.writeToClient(logdapi.NewErrorResponse(clientID, firstErr.Code, firstErr.Message))
		return
	}

	// Reassemble the participants' data into the subtree the client patched, so a
	// split write reports what it stored (auto-generated ids included) like an
	// unsplit one. The write has already committed, so a failure here costs the
	// data, not the commit.
	data, err := joinPatchResults(req.Patch.Path, reported)
	if err != nil {
		s.log.Error("failed to reassemble split patch result", "path", req.Patch.Path, "error", err)
	}
	_ = s.writeToClient(logdapi.NewPatchResponse(clientID, commit, data))
}

// serveNewTx answers a baseline client's NewTx from docd's pre-fetched pool,
// avoiding a logd round trip. The pooled id is a real logd transaction id;
// participants join it by writing to logd with it (WOL).
func (s *ClientSession) serveNewTx(req *logdapi.SessionRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	txID, err := s.server.txPool.Get(ctx, req.NewTx.Participants)
	if err != nil {
		return s.writeToClient(logdapi.NewErrorResponse(req.ID, logdapi.ErrCodeInvalidTx,
			fmt.Sprintf("newtx failed: %v", err)))
	}
	return s.writeToClient(&logdapi.SessionResponse{
		ID:     req.ID,
		Result: &logdapi.SessionResult{NewTx: &logdapi.NewTxResult{TxID: txID}},
	})
}

// routeFor classifies a request: to the owning controller if its path is under a
// live mount, to an "unavailable" error if under a tombstoned mount (controller
// crashed), or to logd for a base/unmounted path or a pathless session op. The
// entry is returned for the controller and unavailable cases.
func (s *ClientSession) routeFor(req *logdapi.SessionRequest) (routeDest, *MountEntry) {
	path := requestPath(req)
	if path == "" {
		return destLogd, nil
	}
	if isMetaPath(path) {
		return destMeta, nil
	}
	entry := s.server.Mounts.LookupPrefix(path)
	if entry == nil {
		return destLogd, nil
	}
	if entry.Live() {
		return destController, entry
	}
	return destUnavailable, entry
}

// writeToLogd encodes and writes a request to the per-client logd connection.
// The request loop, watch coordination goroutines, and force-teardown can all
// write to logd, so writes are serialized (and encoded under the lock, since ir
// encoding mutates node linkage).
func (s *ClientSession) writeToLogd(req *logdapi.SessionRequest) error {
	s.logdWMu.Lock()
	defer s.logdWMu.Unlock()
	data, err := req.ToTony(logdapi.WireOptions()...)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}
	if _, err := s.logd.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write to logd: %w", err)
	}
	return nil
}

// writeToClient encodes and writes a response to the client connection. It is
// called from both the logd pump and controller MountSessions, so writes are
// serialized to keep documents from interleaving on the wire.
func (s *ClientSession) writeToClient(resp *logdapi.SessionResponse) error {
	// Encode under the lock: ir encoding mutates node linkage, so serializing it
	// with the write keeps concurrent forwarders (the logd pump and controller
	// sessions) from racing on a shared node.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	data, err := resp.ToTony(logdapi.WireOptions()...)
	if err != nil {
		return fmt.Errorf("failed to encode response: %w", err)
	}
	// Bound the write so a slow/dead client can't block this call forever — it is invoked
	// synchronously from the mount coordinator's force path and composed-watch forwarding.
	// Write-only deadline: the client read pump is unaffected.
	_ = s.conn.SetWriteDeadline(time.Now().Add(s.writeTimeout))
	if _, err := s.conn.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write to client: %w", err)
	}
	// Record the resume point for a possible force-end: the highest commit delivered
	// on this watch (keyed by watch key, from the event's stamped id; skip the
	// terminal event itself). See lastSeen.
	if ev := resp.Event; ev != nil && ev.Path != "" && ev.Commit > 0 && !ev.Ended {
		key := watchKeyFor(resp.ID, ev.Path)
		s.lastSeenMu.Lock()
		if ev.Commit > s.lastSeen[key] {
			s.lastSeen[key] = ev.Commit
		}
		s.lastSeenMu.Unlock()
	}
	// Everything docd tells a client passes here, so this is where it learns how far
	// the stores behind it have got -- for the pong to pass on. See Server.seen.
	if ev := resp.Event; ev != nil {
		s.server.noteCommit(ev.Commit)
	}
	if r := resp.Result; r != nil {
		if r.Match != nil {
			s.server.noteCommit(r.Match.Commit)
		}
		if r.Patch != nil {
			s.server.noteCommit(r.Patch.Commit)
		}
	}
	return nil
}

// trackMount records that this client has routed to a mount, so its routes can
// be torn down when the client leaves.
func (s *ClientSession) trackMount(m *MountSession) {
	s.mountsMu.Lock()
	s.usedMounts[m] = struct{}{}
	s.mountsMu.Unlock()
}

// cleanupMounts drops this client's routes from every mount it used.
func (s *ClientSession) cleanupMounts() {
	s.mountsMu.Lock()
	mounts := make([]*MountSession, 0, len(s.usedMounts))
	for m := range s.usedMounts {
		mounts = append(mounts, m)
	}
	s.usedMounts = make(map[*MountSession]struct{})
	s.mountsMu.Unlock()

	for _, m := range mounts {
		m.dropClient(s)
	}
}

// connectLogd dials logd, retrying with exponential backoff so that startup
// ordering and brief logd unavailability do not needlessly fail the client. It
// gives up after logdConnectTimeout (the client then reconnects) or when the
// session is closed. Mid-session logd drops are not reconnected here — the
// session tears down and the client's own LogdSession reconnects and replays
// its watches, so docd need not duplicate that resilience.
func (s *ClientSession) connectLogd() (net.Conn, error) {
	backoff := 100 * time.Millisecond
	const maxBackoff = 5 * time.Second
	deadline := time.Now().Add(logdConnectTimeout)

	for {
		select {
		case <-s.done:
			return nil, fmt.Errorf("session closed")
		default:
		}

		conn, err := net.DialTimeout("tcp", s.logdAddr, 5*time.Second)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("logd unavailable at %s: %w", s.logdAddr, err)
		}
		s.log.Debug("logd dial failed, retrying", "addr", s.logdAddr, "error", err, "backoff", backoff)

		select {
		case <-s.done:
			return nil, fmt.Errorf("session closed")
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// Close closes the client connection, which cascades through Run to the logd
// connection and unblocks both pumps, and aborts any in-progress logd dial.
// Safe to call more than once.
func (s *ClientSession) Close() error {
	s.closeOnce.Do(func() { close(s.done) })
	return s.conn.Close()
}

// requestPath returns the path a routable operation targets, or "" for
// operations that carry no single path (hello, newtx, schema, deleteScope).
func requestPath(req *logdapi.SessionRequest) string {
	switch {
	case req.Match != nil:
		return req.Match.Body.Path
	case req.Patch != nil:
		return req.Patch.Path
	case req.Watch != nil:
		return req.Watch.Path
	case req.Unwatch != nil:
		return req.Unwatch.Path
	}
	return ""
}

// ignoreClosed folds the expected end-of-connection signals (EOF, closed conn)
// into a clean stop.
func ignoreClosed(err error) error {
	if err == nil || err == io.EOF || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
