package server

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
)

// TCPListener manages TCP connections for the session protocol.
type TCPListener struct {
	listener net.Listener
	server   *Server
	hub      *WatchHub

	// Session management
	sessions   map[string]*Session
	sessionsMu sync.RWMutex
	sessionSeq atomic.Int64

	// Shutdown
	done   chan struct{}
	wg     sync.WaitGroup
	closed atomic.Bool
}

// NewTCPListener creates a new TCP listener.
func NewTCPListener(addr string, server *Server, hub *WatchHub) (*TCPListener, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	return &TCPListener{
		listener: listener,
		server:   server,
		hub:      hub,
		sessions: make(map[string]*Session),
		done:     make(chan struct{}),
	}, nil
}

// Addr returns the listener's network address.
func (l *TCPListener) Addr() net.Addr {
	return l.listener.Addr()
}

// Serve accepts connections and creates sessions.
// Blocks until Close is called or an error occurs.
func (l *TCPListener) Serve() error {
	l.server.Spec.Log.Info("TCP listener started", "addr", l.listener.Addr().String())

	for {
		conn, err := l.listener.Accept()
		if err != nil {
			if l.closed.Load() {
				return nil // Normal shutdown
			}
			l.server.Spec.Log.Error("accept error", "error", err)
			continue
		}

		l.wg.Add(1)
		go l.handleConnection(conn)
	}
}

// handleConnection creates and runs a session for the connection.
func (l *TCPListener) handleConnection(conn net.Conn) {
	defer l.wg.Done()

	// Generate session ID
	seq := l.sessionSeq.Add(1)
	sessionID := fmt.Sprintf("tcp-%d", seq)

	l.server.Spec.Log.Debug("new TCP connection", "session", sessionID, "remote", conn.RemoteAddr().String())

	// Create session
	session := NewSession(sessionID, conn, &SessionConfig{
		Storage:  l.server.Spec.Storage,
		Hub:      l.hub,
		Log:      l.server.Spec.Log,
		OnCommit: l.server.onCommit,
	})

	// Track session
	l.sessionsMu.Lock()
	l.sessions[sessionID] = session
	l.sessionsMu.Unlock()

	// Run session
	err := session.Run()
	if err != nil {
		l.server.Spec.Log.Error("session error", "session", sessionID, "error", err)
	}

	// Remove session
	l.sessionsMu.Lock()
	delete(l.sessions, sessionID)
	l.sessionsMu.Unlock()

	l.server.Spec.Log.Debug("session ended", "session", sessionID)
}

// Close shuts down the listener and all sessions.
func (l *TCPListener) Close() error {
	if l.closed.Swap(true) {
		return nil // Already closed
	}

	close(l.done)

	// Close listener to stop accepting new connections
	if err := l.listener.Close(); err != nil {
		l.server.Spec.Log.Error("error closing listener", "error", err)
	}

	// Close all active sessions
	l.sessionsMu.RLock()
	for _, session := range l.sessions {
		session.Close()
	}
	l.sessionsMu.RUnlock()

	// Wait for all sessions to complete
	l.wg.Wait()

	l.server.Spec.Log.Info("TCP listener stopped")
	return nil
}

// SessionCount returns the number of active sessions.
func (l *TCPListener) SessionCount() int {
	l.sessionsMu.RLock()
	defer l.sessionsMu.RUnlock()
	return len(l.sessions)
}
