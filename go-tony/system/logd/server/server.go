package server

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

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

	// snapshotting counts snapshots running off the commit path, so shutdown can wait
	// for one rather than closing the store under it.
	snapshotting sync.WaitGroup

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
			spec.Storage.SetTxTimeout(time.Duration(spec.Config.Tx.Timeout))
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
//
// The snapshot itself runs OFF the caller. It used to run here, which meant inside
// handlePatch, before that patch's response was sent: whichever write happened to be
// the thousandth paid for a full snapshot of the store -- plus CheckHead, which is a
// whole-document read -- and every write behind it on that session waited too. On a
// staging store that surfaced as a client deadline on an unremarkable write
// ("context deadline exceeded" on a ref nobody was doing anything unusual with), and
// it lands on a different write every time, which is what made it look random.
//
// A snapshot does not need the writer. Double-buffered logs are exactly what makes
// this safe: SwitchActive flips the active log first, so commits during the snapshot
// land in the new log while the old one is being snapshotted (dvgz9308h12ks4xmgdn0).
func (s *Server) maybeCompact() {
	cfg := s.Spec.Config
	if cfg == nil || cfg.Snapshot == nil {
		return
	}

	// Only one goroutine checks thresholds at a time. The flag is held until the
	// snapshot below finishes, not just until the check does.
	if !s.switching.CompareAndSwap(false, true) {
		return
	}
	released := false
	defer func() {
		if !released {
			s.switching.Store(false)
		}
	}()

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

	if !shouldSwitch {
		return
	}

	s.Spec.Log.Info("triggering snapshot", "commitsSinceSnapshot", commitCount)
	released = true // the goroutine owns the flag now
	s.snapshotting.Add(1)
	go func() {
		defer s.snapshotting.Done()
		defer s.switching.Store(false)
		start := time.Now()
		if err := s.Spec.Storage.SwitchDLog(); err != nil {
			s.Spec.Log.Error("snapshot failed", "error", err, "took", time.Since(start))
			return
		}
		// Subtract what was counted rather than zeroing: writes kept landing while
		// the snapshot ran, and they are commits since THIS snapshot.
		s.commitsSinceSnapshot.Add(-commitCount)
		s.Spec.Log.Info("snapshot complete", "commits", commitCount, "took", time.Since(start))
	}()
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
	// A snapshot triggered by the last commits may still be running off the commit
	// path; the store is closed after this returns, so wait for it rather than pull
	// the log out from under it.
	s.awaitSnapshots()
	return err
}

// awaitSnapshots blocks until any snapshot running off the commit path has finished.
// A caller which needs to observe the effect of a snapshot -- shutdown, or a test --
// waits here rather than polling, since the counter is spawned before the goroutine.
func (s *Server) awaitSnapshots() { s.snapshotting.Wait() }

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
