package server

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync/atomic"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// Server represents the logd HTTP server.
type Server struct {
	Spec Spec

	// WatchHub manages subscriptions across sessions
	Hub *WatchHub

	// TCP listener for session protocol
	tcpListener *TCPListener

	// Session sequence counter for HTTP sessions
	httpSessionSeq atomic.Int64

	// commitsSinceSnapshot tracks commits for snapshot policy
	commitsSinceSnapshot int64
}

// New creates a new Server instance.
func New(spec *Spec) *Server {
	if spec.Log == nil {
		spec.Log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slogLevel(),
		}))
	}
	if spec.Config == nil {
		spec.Config = DefaultConfig()
	}

	s := &Server{
		Spec: *spec,
		Hub:  NewWatchHub(),
	}

	// Wire up commit notifications to the watch hub
	if spec.Storage != nil {
		spec.Storage.SetCommitNotifier(s.Hub.Broadcast)
	}

	return s
}

func slogLevel() slog.Level {
	if os.Getenv("DEBUG") != "" {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/data":
		s.handleData(w, r)
	case "/api/session":
		s.handleSession(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleData handles legacy /api/data MATCH/PATCH requests.
func (s *Server) handleData(w http.ResponseWriter, r *http.Request) {
	// Route based on HTTP method
	switch r.Method {
	case "MATCH":
		d, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("i/o error: %v", err), http.StatusInternalServerError)
			return
		}
		req := &api.Match{}
		if err := req.FromTony(d); err != nil {
			writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidDiff, fmt.Sprintf("failed to parse request body: %v", err)))
			return
		}
		s.handleMatch(w, r, req)
	case "PATCH":
		d, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("i/o error: %v", err), http.StatusInternalServerError)
			return
		}
		req := &api.Patch{}
		if err := req.FromTony(d); err != nil {
			writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidDiff, fmt.Sprintf("failed to parse request body: %v", err)))
			return
		}
		s.handlePatch(w, r, req)
	default:
		writeError(w, http.StatusMethodNotAllowed, api.NewError("method_not_allowed", fmt.Sprintf("method %s not allowed", r.Method)))
	}
}

// handleSession handles POST /api/session requests.
// It hijacks the HTTP connection and runs a bidirectional session.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, api.NewError("method_not_allowed", "POST required for /api/session"))
		return
	}

	// Hijack the connection
	hj, ok := w.(http.Hijacker)
	if !ok {
		writeError(w, http.StatusInternalServerError, api.NewError("hijack_failed", "connection hijacking not supported"))
		return
	}

	conn, bufrw, err := hj.Hijack()
	if err != nil {
		s.Spec.Log.Error("hijack failed", "error", err)
		return
	}

	// Generate session ID
	seq := s.httpSessionSeq.Add(1)
	sessionID := fmt.Sprintf("http-%d", seq)

	s.Spec.Log.Debug("HTTP session started", "session", sessionID, "remote", r.RemoteAddr)

	// Wrap the connection with the buffered reader (may contain buffered data)
	wrappedConn := &hijackedConn{
		Conn: conn,
		r:    bufrw.Reader,
	}

	// Create and run session
	session := NewSession(sessionID, wrappedConn, &SessionConfig{
		Storage:  s.Spec.Storage,
		Hub:      s.Hub,
		Log:      s.Spec.Log,
		OnCommit: s.onCommit,
	})

	// Run session (blocks until session ends)
	if err := session.Run(); err != nil {
		s.Spec.Log.Error("HTTP session error", "session", sessionID, "error", err)
	}

	s.Spec.Log.Debug("HTTP session ended", "session", sessionID)
}

// hijackedConn wraps a hijacked connection with buffered reader.
type hijackedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *hijackedConn) Read(p []byte) (n int, err error) {
	return c.r.Read(p)
}

// handleMatch handles MATCH requests (reads).
func (s *Server) handleMatch(w http.ResponseWriter, r *http.Request, req *api.Match) {
	s.handleMatchData(w, r, req)
}

// handlePatch handles PATCH requests (writes).
func (s *Server) handlePatch(w http.ResponseWriter, r *http.Request, req *api.Patch) {
	s.handlePatchData(w, r, req)
}

// maybeSnapshot checks snapshot thresholds and triggers SwitchAndSnapshot if needed.
// Called after successful commits.
func (s *Server) maybeSnapshot() {
	cfg := s.Spec.Config
	if cfg == nil || cfg.Snapshot == nil {
		return
	}

	snap := cfg.Snapshot
	shouldSnapshot := false

	// Check commit count threshold
	if snap.MaxCommits > 0 && s.commitsSinceSnapshot >= snap.MaxCommits {
		shouldSnapshot = true
	}

	// Check log size threshold
	if !shouldSnapshot && snap.MaxBytes > 0 {
		size, err := s.Spec.Storage.ActiveLogSize()
		if err == nil && size >= snap.MaxBytes {
			shouldSnapshot = true
		}
	}

	if shouldSnapshot {
		s.Spec.Log.Info("triggering snapshot", "commitsSinceSnapshot", s.commitsSinceSnapshot)
		if err := s.Spec.Storage.SwitchAndSnapshot(); err != nil {
			s.Spec.Log.Error("snapshot failed", "error", err)
		} else {
			s.commitsSinceSnapshot = 0
		}
	}
}

// writeError writes an error response.
func writeError(w http.ResponseWriter, statusCode int, err *api.Error) {
	w.Header().Set("Content-Type", "application/x-tony")
	w.WriteHeader(statusCode)

	errorNode := ir.FromMap(map[string]*ir.Node{
		"error": ir.FromMap(map[string]*ir.Node{
			"code":    &ir.Node{Type: ir.StringType, String: err.Code},
			"message": &ir.Node{Type: ir.StringType, String: err.Message},
		}),
	})

	encode.Encode(errorNode, w)
}

// StartTCP starts the TCP listener on the given address.
// The listener runs in a separate goroutine.
func (s *Server) StartTCP(addr string) error {
	if s.tcpListener != nil {
		return fmt.Errorf("TCP listener already running")
	}

	listener, err := NewTCPListener(addr, s, s.Hub)
	if err != nil {
		return err
	}

	s.tcpListener = listener

	go func() {
		if err := listener.Serve(); err != nil {
			s.Spec.Log.Error("TCP listener error", "error", err)
		}
	}()

	return nil
}

// StopTCP stops the TCP listener.
func (s *Server) StopTCP() error {
	if s.tcpListener == nil {
		return nil
	}

	err := s.tcpListener.Close()
	s.tcpListener = nil
	return err
}

// TCPAddr returns the TCP listener's address, or nil if not running.
func (s *Server) TCPAddr() string {
	if s.tcpListener == nil {
		return ""
	}
	return s.tcpListener.Addr().String()
}

// onCommit is called after successful commits for snapshot tracking.
func (s *Server) onCommit() {
	s.commitsSinceSnapshot++
	s.maybeSnapshot()
}
