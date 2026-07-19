package server

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
)

// ClientTCPListener accepts client connections speaking the logd session
// protocol and proxies each to logd via a ClientSession. It is separate from the
// mount-facing TCPListener: docd serves the two protocols on two addresses.
type ClientTCPListener struct {
	listener net.Listener
	server   *Server

	sessions   map[string]*ClientSession
	sessionsMu sync.RWMutex
	sessionSeq atomic.Int64

	done   chan struct{}
	wg     sync.WaitGroup
	closed atomic.Bool
}

// NewClientTCPListener creates a new client-facing TCP listener.
func NewClientTCPListener(addr string, server *Server) (*ClientTCPListener, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	return &ClientTCPListener{
		listener: listener,
		server:   server,
		sessions: make(map[string]*ClientSession),
		done:     make(chan struct{}),
	}, nil
}

// Addr returns the listener's network address.
func (l *ClientTCPListener) Addr() net.Addr {
	return l.listener.Addr()
}

// Serve accepts connections and creates client sessions. It blocks until Close.
func (l *ClientTCPListener) Serve() error {
	l.server.Spec.Log.Info("docd client TCP listener started", "addr", l.listener.Addr().String())

	for {
		conn, err := l.listener.Accept()
		if err != nil {
			if l.closed.Load() {
				return nil // Normal shutdown
			}
			l.server.Spec.Log.Error("client accept error", "error", err)
			continue
		}

		l.wg.Add(1)
		go l.handleConnection(conn)
	}
}

// handleConnection creates and runs a client session for the connection.
func (l *ClientTCPListener) handleConnection(conn net.Conn) {
	defer l.wg.Done()

	seq := l.sessionSeq.Add(1)
	sessionID := fmt.Sprintf("client-%d", seq)

	l.server.Spec.Log.Debug("new client connection", "session", sessionID, "remote", conn.RemoteAddr().String())

	session := NewClientSession(sessionID, conn, &ClientSessionConfig{
		Log:    l.server.Spec.Log,
		Server: l.server,
	})

	l.sessionsMu.Lock()
	l.sessions[sessionID] = session
	l.sessionsMu.Unlock()

	if err := session.Run(); err != nil {
		l.server.Spec.Log.Error("client session error", "session", sessionID, "error", err)
	}

	l.sessionsMu.Lock()
	delete(l.sessions, sessionID)
	l.sessionsMu.Unlock()

	l.server.Spec.Log.Debug("client session ended", "session", sessionID)
}

// Close shuts down the listener and all client sessions.
func (l *ClientTCPListener) Close() error {
	if l.closed.Swap(true) {
		return nil // Already closed
	}

	close(l.done)

	if err := l.listener.Close(); err != nil {
		l.server.Spec.Log.Error("error closing client listener", "error", err)
	}

	l.sessionsMu.RLock()
	for _, session := range l.sessions {
		session.Close()
	}
	l.sessionsMu.RUnlock()

	l.wg.Wait()

	l.server.Spec.Log.Info("docd client TCP listener stopped")
	return nil
}

// SessionCount returns the number of active client sessions.
func (l *ClientTCPListener) SessionCount() int {
	l.sessionsMu.RLock()
	defer l.sessionsMu.RUnlock()
	return len(l.sessions)
}
