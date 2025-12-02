package storage

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/compact"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/dfile"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/paths"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/seq"
)

// Storage provides filesystem-based storage for logd.
type Storage struct {
	*seq.Seq
	umask      int // Umask to apply when creating directories
	FS         *FS
	logMu      sync.RWMutex
	log        *slog.Logger // Logger for error logging
	index      *index.Index
	indexMu    sync.RWMutex // Protects index + filesystem consistency
	txLocks    sync.Map
	compactor  *compact.Compactor
	stateCache *stateCache // Heat-based cache for compacted and computed states
}

// Open opens or creates a Storage instance with the given root directory.
// The root directory will be created if it doesn't exist.
// umask is applied to directory permissions (e.g., 022 for 0755 -> 0755).
// If logger is nil, slog.Default() will be used.
// compactorConfig is optional - if nil, a default config with divisor 2 and NeverRemove is used.
func Open(root string, umask int, logger *slog.Logger, compactorConfig *compact.Config) (*Storage, error) {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Storage{
		Seq:   seq.NewSeq(root),
		FS:    &FS{Root: root},
		umask: umask,
		log:   logger,
		index: index.NewIndex(""),
	}
	if err := s.init(); err != nil {
		return nil, err
	}

	// Initialize state cache with defaults:
	// - sizeThreshold: 10MB (baseline for size penalty)
	// - softLimit: 100MB (start evicting)
	// - hardLimit: 200MB (aggressive eviction)
	const (
		sizeThreshold = 10 * 1024 * 1024  // 10MB
		softLimit     = 100 * 1024 * 1024 // 100MB
		hardLimit     = 200 * 1024 * 1024 // 200MB
	)
	s.stateCache = newStateCache(sizeThreshold, softLimit, hardLimit)

	// Initialize compactor
	if compactorConfig == nil {
		compactorConfig = &compact.Config{
			Root:    root,
			Divisor: 2,
			Remove:  compact.NeverRemove(),
			Log:     logger,
		}
	} else {
		// Ensure Root and Log are set
		if compactorConfig.Root == "" {
			compactorConfig.Root = root
		}
		if compactorConfig.Log == nil {
			compactorConfig.Log = logger
		}
	}
	// Set callback to update cache when compaction completes
	originalCallback := compactorConfig.OnCompactionComplete
	compactorConfig.OnCompactionComplete = func(virtualPath string, ref *ir.Node, endCommit int64) {
		// Call original callback if set
		if originalCallback != nil {
			originalCallback(virtualPath, ref, endCommit)
		}
		// Update cache
		s.updateCompactedState(virtualPath, ref, endCommit)
	}
	s.compactor = compact.NewCompactor(compactorConfig, s.Seq, &s.indexMu, s.index)

	return s, nil
}

// ListChildPaths returns all immediate child paths under parentPath that have
// diffs in the commit range [from, to]. Pass nil for unbounded range.
func (s *Storage) ListChildPaths(parentPath string, from, to *int64) ([]string, error) {
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()

	// Navigate to the index node for parentPath
	idx := s.index
	if parentPath != "" && parentPath != "/" {
		parts := strings.Split(strings.Trim(parentPath, "/"), "/")
		for _, part := range parts {
			if part == "" {
				continue
			}
			idx.RLock()
			child := idx.Children[part]
			idx.RUnlock()
			if child == nil {
				return nil, nil
			}
			idx = child
		}
	}

	// Get all children at this level
	return idx.ListRange(from, to), nil
}

// commit atomically commits a pending diff by:
// 1. Renaming .pending to .diff
// 2. Updating the index
func (s *Storage) commit(virtualPath string, txSeq, commitCount int64) error {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	// Rename pending file to diff file
	fsPath := s.FS.PathToFilesystem(virtualPath)
	pendingSeg := index.PointLogSegment(0, txSeq, "")
	if err := dfile.CommitPending(fsPath, pendingSeg, 0, commitCount); err != nil {
		return err
	}

	// Update index
	seg := index.PointLogSegment(commitCount, txSeq, virtualPath)
	s.index.Add(seg)

	// Notify compactor of new segment
	if s.compactor != nil {
		if err := s.compactor.OnNewSegment(seg); err != nil {
			// Log but don't fail - compaction is best effort
			s.log.Warn("failed to notify compactor of new segment", "error", err, "path", virtualPath, "commit", commitCount)
		}
	}

	return nil
}

// deletePathAt deletes a pending diff file.
func (s *Storage) deletePathAt(virtualPath string, txSeq int64) error {
	fsPath := s.FS.PathToFilesystem(virtualPath)
	seg := index.PointLogSegment(0, txSeq, "")
	return dfile.DeletePending(fsPath, seg, 0)
}

// GetCurrentCommit returns the current commit number.
// This is a snapshot - if commits happen after this call, they won't be reflected.
func (s *Storage) GetCurrentCommit() (int64, error) {
	s.Lock()
	defer s.Unlock()
	state, err := s.ReadStateLocked()
	if err != nil {
		return 0, err
	}
	commit := state.Commit
	// Update cache's current commit for recency calculations
	if s.stateCache != nil {
		s.stateCache.updateCurrentCommit(commit)
	}
	return commit, nil
}

// ReadStateAt reads the state for a given virtual path at a specific commit count.
// It uses compaction and caching when available to minimize the number of patches applied.
// If commitCount is 0, reads the latest state (all segments).
func (s *Storage) ReadStateAt(virtualPath string, commitCount int64) (*ir.Node, error) {
	// Update current commit for cache recency calculations (only if we have a specific commit)
	if commitCount > 0 && s.stateCache != nil {
		s.stateCache.updateCurrentCommit(commitCount)
	}

	// Tier 1: Check computed cache for exact (path, commit) match
	if s.stateCache != nil && commitCount > 0 {
		cached := s.stateCache.getComputed(virtualPath, commitCount)
		if cached != nil {
			s.stateCache.recordHit(cached)
			return cached.Value, nil
		}
	}

	// Tier 2: Try to use compacted state as starting point
	var startState *ir.Node
	var startCommit int64 = 0

	if s.stateCache != nil {
		cached := s.stateCache.getCompacted(virtualPath)
		if cached != nil {
			// Use cached compacted state, but only if it's before our target commit
			if commitCount == 0 || cached.EndCommit <= commitCount {
				startState = cached.Value
				startCommit = cached.EndCommit
			}
		}
	}

	// If no compacted state, start from null
	if startState == nil {
		startState = ir.Null()
	}

	// Use LookupRange to efficiently get only the segments we need:
	// - from: startCommit+1 (segments after compacted state)
	// - to: commitCount (or nil if reading latest)
	var from *int64
	if startCommit > 0 {
		fromCommit := startCommit + 1
		from = &fromCommit
	}
	var to *int64
	if commitCount > 0 {
		to = &commitCount
	}

	s.indexMu.RLock()
	segments := s.index.LookupRange(virtualPath, from, to)
	s.indexMu.RUnlock()

	if len(segments) == 0 {
		// No patches to apply - cache the result
		if s.stateCache != nil && commitCount > 0 {
			s.stateCache.setComputed(virtualPath, commitCount, startState)
		}
		return startState, nil
	}

	// Apply segments returned by LookupRange
	state := startState
	for i := range segments {
		seg := &segments[i]
		diffFile, err := s.ReadDiff(virtualPath, seg.StartCommit, seg.StartTx, false)
		if err != nil {
			return nil, fmt.Errorf("failed to read diff at commit %d: %w", seg.StartCommit, err)
		}

		state, err = tony.Patch(state, diffFile.Diff)
		if err != nil {
			return nil, fmt.Errorf("failed to apply diff at commit %d: %w", seg.StartCommit, err)
		}
	}

	// Cache the computed result
	if s.stateCache != nil && commitCount > 0 {
		s.stateCache.setComputed(virtualPath, commitCount, state)
	}

	return state, nil
}

// ReadCurrentState reads the current committed state for a given virtual path.
// This is equivalent to calling GetCurrentCommit() then ReadStateAt() with that commit.
// If commits happen between getting the commit and reading, they are ignored (point-in-time read).
func (s *Storage) ReadCurrentState(virtualPath string) (*ir.Node, error) {
	commit, err := s.GetCurrentCommit()
	if err != nil {
		return nil, fmt.Errorf("failed to get current commit: %w", err)
	}
	return s.ReadStateAt(virtualPath, commit)
}

// updateCompactedState updates the cached compacted state for a virtual path.
// This is called by the compactor when compaction completes.
func (s *Storage) updateCompactedState(virtualPath string, ref *ir.Node, endCommit int64) {
	if s.stateCache != nil {
		s.stateCache.setCompacted(virtualPath, ref, endCommit)
	}
}

// clearCompactedState removes the cached compacted state for a virtual path.
// This can be called when a path is deleted or when cache cleanup is needed.
func (s *Storage) clearCompactedState(virtualPath string) {
	if s.stateCache != nil {
		s.stateCache.clearCompacted(virtualPath)
		s.stateCache.clearComputed(virtualPath)
	}
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
			seg, _, err := paths.ParseLogSegment(filepath.Join(virtualPath, base))
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
		state := &seq.State{Commit: 0, TxSeq: 0}
		err := s.WriteStateLocked(state)
		s.Seq.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}
