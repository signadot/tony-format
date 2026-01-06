package server

import (
	"fmt"
	"log/slog"
	"os"
)

// Server represents the docd document server.
type Server struct {
	Spec Spec

	// TCP listener for client connections
	tcpListener *TCPListener
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
	if spec.LogdAddr == "" {
		spec.LogdAddr = spec.Config.LogdAddr
	}

	return &Server{
		Spec: *spec,
	}
}

func slogLevel() slog.Level {
	if os.Getenv("DEBUG") != "" {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

// StartTCP starts the TCP listener on the given address.
// The listener runs in a separate goroutine.
func (s *Server) StartTCP(addr string) error {
	if s.tcpListener != nil {
		return fmt.Errorf("TCP listener already running")
	}

	listener, err := NewTCPListener(addr, s)
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

// TCPAddr returns the TCP listener's address, or empty string if not running.
func (s *Server) TCPAddr() string {
	if s.tcpListener == nil {
		return ""
	}
	return s.tcpListener.Addr().String()
}
