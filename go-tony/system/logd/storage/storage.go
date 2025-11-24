package storage

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/seq"
)

// Storage provides filesystem-based storage for logd.
type Storage struct {
	*seq.Seq
	umask int // Umask to apply when creating directories
	FS    *FS
	logMu sync.RWMutex
	log   *slog.Logger // Logger for error logging
	index *index.Index
}

// Open opens or creates a Storage instance with the given root directory.
// The root directory will be created if it doesn't exist.
// umask is applied to directory permissions (e.g., 022 for 0755 -> 0755).
// If logger is nil, slog.Default() will be used.
func Open(root string, umask int, logger *slog.Logger) (*Storage, error) {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Storage{Seq: seq.NewSeq(root), FS: &FS{Root: root}, umask: umask, log: logger, index: index.NewIndex("")}
	if err := s.init(); err != nil {
		return nil, err
	}
	return s, nil
}

// ListChildPaths returns all immediate child paths under parentPath.
func (s *Storage) ListChildPaths(parentPath string) ([]string, error) {
	return s.FS.ListChildPaths(parentPath)
}

// CommitPendingDiff atomically commits a pending diff by:
// 1. Renaming .pending to .diff
// 2. Updating the index
func (s *Storage) CommitPendingDiff(virtualPath string, txSeq, commitCount int64) error {
	// Rename pending file to diff file
	if err := s.FS.RenamePendingToDiff(virtualPath, commitCount, txSeq); err != nil {
		return err
	}

	// Update index
	s.index.Add(index.PointLogSegment(commitCount, txSeq, virtualPath))

	return nil
}

// DeletePendingDiff deletes a pending diff file.
func (s *Storage) DeletePendingDiff(virtualPath string, txSeq int64) error {
	return s.FS.DeletePending(virtualPath, txSeq)
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
		filepath.Join(s.Root, "meta"),
		filepath.Join(s.Root, "meta", "transactions"),
		filepath.Join(s.Root, "paths"),
	}

	for _, dir := range dirs {
		if err := s.mkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	// Rebuild index from filesystem
	idx, err := index.Build(
		filepath.Join(s.Root, "paths"),
		func(path string) (*index.LogSegment, error) {
			dir, base := filepath.Split(path)
			virtualPath := s.FS.FilesystemToPath(dir)
			seg, err := s.FS.ParseLogSegment(filepath.Join(virtualPath, base))
			if err != nil {
				s.log.Warn("error getting parse log segment", "error", err, "fs", path, "virt", virtualPath)
				return nil, err
			}
			return seg, nil
		},
	)
	if err != nil {
		return err
	}
	s.index = idx

	// Initialize sequence number file if it doesn't exist
	seqFile := filepath.Join(s.Root, "meta", "seq")
	if _, err := os.Stat(seqFile); os.IsNotExist(err) {
		s.Seq.Lock()
		state := &seq.State{CommitCount: 0, TxSeq: 0}
		err := s.WriteStateLocked(state)
		s.Seq.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}
