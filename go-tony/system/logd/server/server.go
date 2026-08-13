package server

import (
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
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

	// switching guards threshold check + SwitchDLog as one atomic operation.
	// Only one goroutine checks and potentially switches at a time.
	switching atomic.Bool
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
	} else {
		// A config built in code can be as partial as one written in a file, and a
		// missing snapshot section means the same thing in both: nobody said, not
		// nobody wants one.
		spec.Config = spec.Config.WithDefaults()
	}

	// Say what the snapshot policy is on the way up. A store that never snapshots
	// has no symptom until its reads take seconds, so the one moment it can be
	// noticed for free is here.
	if snap := spec.Config.Snapshot; snap != nil {
		spec.Log.Info("snapshot policy", "maxCommits", snap.MaxCommits, "maxBytes", snap.MaxBytes)
	} else {
		spec.Log.Warn("snapshotting disabled: the delta log will grow without bound")
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

		// Set up schema resolver if schema is configured
		if spec.Config.Schema != nil {
			schema := api.ParseSchemaFromNode(spec.Config.Schema)
			if schema != nil {
				spec.Storage.SetSchemaResolver(&api.StaticSchemaResolver{Schema: schema})
				spec.Log.Info("configured schema", "autoIDFields", len(schema.AutoIDFields))
			}
		}

		// Set storage durability if configured
		if spec.Config.Storage != nil {
			d, err := spec.Config.Storage.ToStorageDurability()
			if err != nil {
				// LoadConfig rejects this, so getting here means a Config built in
				// code. Keep the storage default and say so, rather than guess which
				// way the caller meant to err.
				spec.Log.Error("invalid storage durability; keeping default",
					"error", err, "durability", spec.Storage.GetDurability())
			} else {
				spec.Storage.SetDurability(d)
				spec.Log.Info("configured storage durability", "durability", d)
			}
		}

		// Set up compaction if configured
		if spec.Config.Compaction != nil {
			spec.Storage.SetCompactionConfig(spec.Config.Compaction.ToStorageConfig())
			spec.Log.Info("configured compaction",
				"cutoff", spec.Config.Compaction.Cutoff,
				"slotsPerTier", spec.Config.Compaction.SlotsPerTier)
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

// maybeCompact checks snapshot thresholds and triggers SwitchDLog if needed.
// Called after successful commits. Uses CAS on switching to ensure only one
// goroutine checks thresholds and potentially switches at a time.
func (s *Server) maybeCompact() {
	cfg := s.Spec.Config
	if cfg == nil || cfg.Snapshot == nil {
		return
	}

	// Only one goroutine checks thresholds at a time
	if !s.switching.CompareAndSwap(false, true) {
		return
	}
	defer s.switching.Store(false)

	snap := cfg.Snapshot
	shouldSwitch := false

	// Check commit count threshold
	commitCount := s.commitsSinceSnapshot.Load()
	if snap.MaxCommits > 0 && commitCount >= snap.MaxCommits {
		shouldSwitch = true
	}

	// Check log size threshold — against the delta accumulated since the last
	// snapshot, which is what a read has to replay. Measured on the file, so unlike
	// the commit count above it means the same thing after a restart.
	if !shouldSwitch && snap.MaxBytes > 0 {
		delta, err := s.Spec.Storage.DeltaBytesSinceSnapshot()
		if err != nil {
			s.Spec.Log.Error("failed to size the delta log; snapshot policy is running on commits alone",
				"error", err)
		} else if delta >= snap.MaxBytes {
			shouldSwitch = true
		}
	}

	if shouldSwitch {
		s.Spec.Log.Info("triggering snapshot", "commitsSinceSnapshot", commitCount)
		if err := s.Spec.Storage.SwitchDLog(); err != nil {
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
	s.maybeCompact()
}
