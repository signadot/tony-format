package server

import (
	"io"
	"log/slog"
	"sync"
)

// MountSession represents a controller mount connection to docd.
// docd has two faces - this will evolve to handle both:
//   - Mount-facing sessions (controllers)
type MountSession struct {
	ID   string
	conn io.ReadWriteCloser
	log  *slog.Logger

	done      chan struct{}
	closeOnce sync.Once
}

// MountSessionConfig contains configuration for creating a session.
type MountSessionConfig struct {
	Log *slog.Logger
}

// NewMountSession creates a new session for the given connection.
func NewMountSession(id string, conn io.ReadWriteCloser, cfg *MountSessionConfig) *MountSession {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &MountSession{
		ID:   id,
		conn: conn,
		log:  log.With("session", id),
		done: make(chan struct{}),
	}
}

// Run starts the session and blocks until it completes.
// For now, this is a minimal stub - actual protocol handling will be added.
func (s *MountSession) Run() error {
	defer s.conn.Close()

	// TODO: Implement mount-facing and user-facing protocol handling
	// For now, just wait until closed
	<-s.done
	return nil
}

// Close signals the session to shut down.
func (s *MountSession) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
	})
	return s.conn.Close()
}
