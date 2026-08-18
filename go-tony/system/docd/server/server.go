package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/signadot/tony-format/go-tony/system/docd/txpool"
)

// Server represents the docd document server.
type Server struct {
	Spec Spec

	// Mount registry for controller registrations
	Mounts *MountRegistry

	// Clocks holds docd-driven virtual system clocks (see clock.go). A clock is
	// established over a mount connection via MountHello.Clock and served directly
	// by docd rather than routed to a controller.
	Clocks *clockRegistry

	// coord serializes mount/unmount against active watches so a composed watch's
	// mount membership stays fixed for its lifetime (see mountCoord).
	coord *mountCoord

	// TCP listener for controller (mount) connections
	tcpListener *TCPListener

	// TCP listener for client connections (logd session protocol)
	clientListener *ClientTCPListener

	// Pre-fetched transaction ids from logd, used to serve client NewTx with
	// fewer hops when coordinating multi-participant (multi-mount) writes.
	txPool       *txpool.Pool
	txPoolCancel context.CancelFunc

	// seen is the highest commit docd has told any client about, over every session:
	// reads it answered, writes it reported, watch events it forwarded. A client
	// asking "has anything happened" gets it on a pong, which is cheaper and more
	// current than holding a watch open to be told (7qayp3hah12kscx2gdn0).
	//
	// It is monotonic and it chases the head. It is NOT a store head: docd composes
	// mounts with independent commit sequences, so the number names no single store's
	// state and must not be handed back as a commit to read at.
	seen atomic.Int64
}

// noteCommit raises the high-water mark docd reports on a pong.
func (s *Server) noteCommit(commit int64) {
	for {
		cur := s.seen.Load()
		if commit <= cur || s.seen.CompareAndSwap(cur, commit) {
			return
		}
	}
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
		Clocks: newClockRegistry(),
		coord:  newMountCoord(),
		txPool: txpool.New(&txpool.Config{
			LogdAddr: spec.LogdAddr,
			Log:      spec.Log,
		}),
	}
}

// mountForceAfter resolves the configured drain timeout, substituting the
// built-in default when the spec leaves it unset.
func (s *Server) mountForceAfter() time.Duration {
	if s.Spec.MountForceAfter <= 0 {
		return defaultMountForceAfter
	}
	return s.Spec.MountForceAfter
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

	// Warm the transaction-id pool in the background so NewTx can be served
	// without a logd round trip. Get() also auto-connects, so this is best-effort.
	ctx, cancel := context.WithCancel(context.Background())
	s.txPoolCancel = cancel
	go func() {
		if err := s.txPool.Connect(ctx); err != nil {
			return // cancelled or unreachable; Get() will retry on demand
		}
		s.txPool.Prefetch(ctx, 1, 2, 3)
	}()

	return nil
}

// StopClientTCP stops the client-facing TCP listener and the transaction pool.
func (s *Server) StopClientTCP() error {
	if s.clientListener == nil {
		return nil
	}

	if s.txPoolCancel != nil {
		s.txPoolCancel()
		s.txPoolCancel = nil
	}
	s.txPool.Close()

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
