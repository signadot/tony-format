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

	"github.com/signadot/tony-format/go-tony/gomap"
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

	writeMu sync.Mutex // serializes writes to the client connection

	mountsMu   sync.Mutex
	usedMounts map[*MountSession]struct{} // mounts this client routed to, for teardown

	done      chan struct{} // closed on Close, to abort the logd dial-retry
	closeOnce sync.Once
}

// logdConnectTimeout bounds how long a client session retries dialing logd
// before giving up (and failing the client, which reconnects). Generous enough
// to cover startup ordering and brief logd unavailability.
const logdConnectTimeout = 30 * time.Second

// ClientSessionConfig contains configuration for creating a client session.
type ClientSessionConfig struct {
	Log    *slog.Logger
	Server *Server
}

// NewClientSession creates a new client-facing session for the given connection.
func NewClientSession(id string, conn net.Conn, cfg *ClientSessionConfig) *ClientSession {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &ClientSession{
		id:         id,
		conn:       conn,
		server:     cfg.Server,
		log:        log.With("session", id),
		logdAddr:   cfg.Server.Spec.LogdAddr,
		usedMounts: make(map[*MountSession]struct{}),
		done:       make(chan struct{}),
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

	s.cleanupMounts()
	return firstErr
}

// routeClientRequests reads requests from the client and routes each to the
// owning controller or, for base/session operations, to logd.
func (s *ClientSession) routeClientRequests() error {
	for {
		node, err := decodeDocument(s.clientDec)
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
		// Serve a baseline NewTx from docd's pre-fetched pool (fewer hops). A
		// scoped NewTx falls through to logd on the client's scoped connection,
		// since pooled ids are baseline-scoped.
		if req.NewTx != nil && s.clientScope == nil {
			if err := s.serveNewTx(&req); err != nil {
				return err
			}
			continue
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
		node, err := decodeDocument(s.logdDec)
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
// Only routeClientRequests writes to logd, so no serialization is needed.
func (s *ClientSession) writeToLogd(req *logdapi.SessionRequest) error {
	data, err := req.ToTony(gomap.EncodeWire(true))
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
	data, err := resp.ToTony(gomap.EncodeWire(true))
	if err != nil {
		return fmt.Errorf("failed to encode response: %w", err)
	}
	if _, err := s.conn.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write to client: %w", err)
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

// decodeDocument reads events from the decoder until a complete document is
// available.
func decodeDocument(decoder *stream.Decoder) (*ir.Node, error) {
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

// ignoreClosed folds the expected end-of-connection signals (EOF, closed conn)
// into a clean stop.
func ignoreClosed(err error) error {
	if err == nil || err == io.EOF || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
