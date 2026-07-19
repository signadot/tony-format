package server

import (
	"fmt"
	"log/slog"
	"os"
)

// Server represents the docd document server.
type Server struct {
	Spec Spec

	// Mount registry for controller registrations
	Mounts *MountRegistry

	// TCP listener for controller (mount) connections
	tcpListener *TCPListener

	// TCP listener for client connections (logd session protocol)
	clientListener *ClientTCPListener
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
		Spec:   *spec,
		Mounts: NewMountRegistry(),
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

// TCPAddr returns the mount TCP listener's address, or empty string if not
// running.
func (s *Server) TCPAddr() string {
	if s.tcpListener == nil {
		return ""
	}
	return s.tcpListener.Addr().String()
}

// StartClientTCP starts the client-facing TCP listener on the given address.
// Clients speak the logd session protocol; docd proxies each connection to logd
// (base paths) and, once mounts exist, routes mounted subtrees to controllers.
// The listener runs in a separate goroutine.
func (s *Server) StartClientTCP(addr string) error {
	if s.clientListener != nil {
		return fmt.Errorf("client TCP listener already running")
	}

	listener, err := NewClientTCPListener(addr, s)
	if err != nil {
		return err
	}

	s.clientListener = listener

	go func() {
		if err := listener.Serve(); err != nil {
			s.Spec.Log.Error("client TCP listener error", "error", err)
		}
	}()

	return nil
}

// StopClientTCP stops the client-facing TCP listener.
func (s *Server) StopClientTCP() error {
	if s.clientListener == nil {
		return nil
	}

	err := s.clientListener.Close()
	s.clientListener = nil
	return err
}

// ClientTCPAddr returns the client TCP listener's address, or empty string if
// not running.
func (s *Server) ClientTCPAddr() string {
	if s.clientListener == nil {
		return ""
	}
	return s.clientListener.Addr().String()
}
