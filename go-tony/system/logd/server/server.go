package server

import (
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
)

// Server represents the logd server.
type Server struct {
	Spec Spec

	// WatchHub manages subscriptions across sessions
	Hub *WatchHub

	// TCP listener for session protocol
	tcpListener *TCPListener

	// commitsSinceSnapshot tracks commits for snapshot policy (accessed from multiple goroutines)
	commitsSinceSnapshot atomic.Int64
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

		// Set transaction timeout from config
		if spec.Config.Tx != nil && spec.Config.Tx.Timeout > 0 {
			spec.Storage.SetTxTimeout(spec.Config.Tx.Timeout)
		}
	}

	return s
}

func slogLevel() slog.Level {
	if os.Getenv("DEBUG") != "" {
		return slog.LevelDebug
	}
	return slog.LevelInfo
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
	commitCount := s.commitsSinceSnapshot.Load()
	if snap.MaxCommits > 0 && commitCount >= snap.MaxCommits {
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
		s.Spec.Log.Info("triggering snapshot", "commitsSinceSnapshot", commitCount)
		if err := s.Spec.Storage.SwitchAndSnapshot(); err != nil {
			s.Spec.Log.Error("snapshot failed", "error", err)
		} else {
			s.commitsSinceSnapshot.Store(0)
		}
	}
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
	s.commitsSinceSnapshot.Add(1)
	s.maybeSnapshot()
}
