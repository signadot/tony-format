package server

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"
)

// ClientSession proxies a client connection speaking the logd session protocol
// through docd to logd.
//
// M1a scope (this file): docd is a transparent pass-through. Both the client and
// logd already implement the full async session protocol — id-correlated
// requests/responses, unsolicited watch events, the hello handshake — so docd
// shuttles bytes between the two without interpreting them. Each client
// connection is backed by its own logd connection (1:1), which is why no
// docd-side demultiplexing is needed: the client and logd manage ids and watches
// end to end. This proves switchability — a libctl.LogdSession pointed at docd's
// client address behaves exactly as if pointed at logd.
//
// M1b will replace the client->logd direction with path-based routing: ops under
// a mounted subtree go to the owning controller, while base/unmounted paths
// continue straight to logd. The seam is clientToLogd; logdToClient stays a
// straight copy until controller-originated traffic needs to be merged in.
type ClientSession struct {
	id       string
	conn     net.Conn
	server   *Server
	log      *slog.Logger
	logdAddr string
}

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
		id:       id,
		conn:     conn,
		server:   cfg.Server,
		log:      log.With("session", id),
		logdAddr: cfg.Server.Spec.LogdAddr,
	}
}

// Run dials logd and pumps traffic in both directions until either side closes
// or errors. It blocks until the session ends.
func (s *ClientSession) Run() error {
	defer s.conn.Close()

	logdConn, err := net.DialTimeout("tcp", s.logdAddr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to logd at %s: %w", s.logdAddr, err)
	}
	defer logdConn.Close()

	s.log.Debug("client session proxying to logd", "logd", s.logdAddr)

	// Two independent directions. Whichever finishes first tears down both
	// connections, which unblocks the other pump.
	errc := make(chan error, 2)
	go func() { errc <- s.clientToLogd(logdConn) }()
	go func() { errc <- s.logdToClient(logdConn) }()

	firstErr := <-errc
	s.conn.Close()
	logdConn.Close()
	<-errc // wait for the second pump to drain

	return firstErr
}

// clientToLogd forwards bytes from the client to logd. In M1a this is a straight
// copy; M1b replaces it with path-aware routing.
func (s *ClientSession) clientToLogd(logdConn net.Conn) error {
	return pumpConn(logdConn, s.conn)
}

// logdToClient forwards logd's responses and watch events back to the client.
func (s *ClientSession) logdToClient(logdConn net.Conn) error {
	return pumpConn(s.conn, logdConn)
}

// pumpConn copies src into dst until EOF or error, treating a closed connection
// (the normal teardown signal) as a clean stop. io.Copy reports EOF as a nil
// error, so only an explicit close needs to be folded into a clean stop.
func pumpConn(dst, src net.Conn) error {
	_, err := io.Copy(dst, src)
	if err == nil || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// Close closes the client connection, which cascades through Run to the logd
// connection and unblocks both pumps. Safe to call more than once.
func (s *ClientSession) Close() error {
	return s.conn.Close()
}
