package storage

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// Storage provides filesystem-based storage for logd.
type Storage struct {
	root   string      // Root directory (e.g., "/logd")
	umask  int         // Umask to apply when creating directories
	seqMu  sync.Mutex  // Protects sequence number operations
	logger *slog.Logger // Logger for error logging
}

// Open opens or creates a Storage instance with the given root directory.
// The root directory will be created if it doesn't exist.
// umask is applied to directory permissions (e.g., 022 for 0755 -> 0755).
// If logger is nil, slog.Default() will be used.
func Open(root string, umask int, logger *slog.Logger) (*Storage, error) {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Storage{root: root, umask: umask, logger: logger}
	if err := s.init(); err != nil {
		return nil, err
	}
	return s, nil
}

// mkdirAll creates a directory with umask applied.
func (s *Storage) mkdirAll(path string, perm os.FileMode) error {
	// Apply umask: perm &^ umask
	maskedPerm := perm &^ os.FileMode(s.umask)
	return os.MkdirAll(path, maskedPerm)
}

// init initializes the storage directory structure.
func (s *Storage) init() error {
	dirs := []string{
		filepath.Join(s.root, "meta"),
		filepath.Join(s.root, "meta", "transactions"),
		filepath.Join(s.root, "paths"),
		filepath.Join(s.root, "snapshots"),
	}

	for _, dir := range dirs {
		if err := s.mkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	// Initialize sequence number file if it doesn't exist
	seqFile := filepath.Join(s.root, "meta", "seq")
	if _, err := os.Stat(seqFile); os.IsNotExist(err) {
		s.seqMu.Lock()
		state := &SeqState{CommitCount: 0, TxSeq: 0}
		err := s.writeSeqStateLocked(state)
		s.seqMu.Unlock()
		if err != nil {
			return err
		}
	}

	return nil
}

// Root returns the root directory path.
func (s *Storage) Root() string {
	return s.root
}
