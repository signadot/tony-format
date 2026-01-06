package libctl

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// LogdSession manages a controller's session with logd.
// It handles connection, reconnection with backoff, and the session protocol.
type LogdSession struct {
	addr     string
	clientID string
	log      *slog.Logger

	mu        sync.Mutex
	conn      net.Conn
	decoder   *stream.Decoder
	connected bool
	serverID  string

	// For shutdown
	done   chan struct{}
	closed bool
}

// LogdSessionConfig contains configuration for connecting to logd.
type LogdSessionConfig struct {
	// Addr is the address of logd (e.g., "localhost:9091")
	Addr string

	// ClientID identifies this client to logd
	ClientID string

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

	return &LogdSession{
		addr:     cfg.Addr,
		clientID: cfg.ClientID,
		log:      log.With("component", "logd-session"),
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

		// Send hello
		if err := s.sendHello(conn); err != nil {
			conn.Close()
			return fmt.Errorf("hello failed: %w", err)
		}

		// Read hello response
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
		s.decoder = decoder
		s.connected = true
		s.serverID = resp.Result.Hello.ServerID
		s.log.Info("connected to logd", "addr", s.addr, "serverID", s.serverID)
		return nil
	}
}

// sendHello sends the hello message to logd.
func (s *LogdSession) sendHello(conn net.Conn) error {
	req := &api.SessionRequest{
		Hello: &api.Hello{
			ClientID: s.clientID,
		},
	}
	return s.sendRequestTo(conn, req)
}

// ensureConnected checks connection and reconnects if needed.
func (s *LogdSession) ensureConnected(ctx context.Context) error {
	if s.connected {
		return nil
	}
	return s.connectLocked(ctx)
}

// disconnect marks the connection as broken (must hold mutex).
func (s *LogdSession) disconnect() {
	s.connected = false
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
	s.decoder = nil
}

// Match performs a match query at the given path.
func (s *LogdSession) Match(ctx context.Context, path string) (*ir.Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureConnected(ctx); err != nil {
		return nil, err
	}

	req := &api.SessionRequest{
		Match: &api.MatchRequest{
			Body: api.PathData{
				Path: path,
			},
		},
	}

	resp, err := s.doRequest(req)
	if err != nil {
		s.disconnect()
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
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureConnected(ctx); err != nil {
		return err
	}

	req := &api.SessionRequest{
		Patch: &api.PatchRequest{
			PathData: api.PathData{
				Path: path,
				Data: data,
			},
		},
	}

	resp, err := s.doRequest(req)
	if err != nil {
		s.disconnect()
		return err
	}

	if resp.Error != nil {
		return fmt.Errorf("patch error: %s", resp.Error.Message)
	}

	return nil
}

// doRequest sends a request and reads the response (must hold mutex).
func (s *LogdSession) doRequest(req *api.SessionRequest) (*api.SessionResponse, error) {
	if err := s.sendRequestTo(s.conn, req); err != nil {
		return nil, err
	}
	return s.readResponseWith(s.decoder)
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

// readResponseWith reads a response using the given decoder.
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

// Connected returns whether the session is currently connected.
func (s *LogdSession) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}

// Close shuts down the session and closes the connection.
func (s *LogdSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true
	close(s.done)

	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}
