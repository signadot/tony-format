package storage

import (
	"path/filepath"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/dfile"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/paths"
)

// WriteDiffAtomically writes a diff file to disk.
// commitCount should be 0 for pending files, and the actual commit count for committed files.
// If pending is true, writes as .pending file; otherwise writes as .diff file.
// WriteDiffAtomically atomically allocates sequence numbers and writes the diff file.
// This ensures that files are written in the order that sequence numbers are allocated,
// preventing race conditions where a later sequence number is written before an earlier one.
func (s *Storage) WriteDiffAtomically(virtualPath string, timestamp string, diff *ir.Node, pending bool) (commit, txSeq int64, err error) {
	s.Seq.Lock()
	defer s.Seq.Unlock()

	// Get current state and increment both sequence numbers
	state, err := s.Seq.ReadStateLocked()
	if err != nil {
		return 0, 0, err
	}

	state.Commit++
	state.TxSeq++

	commit = state.Commit
	txSeq = state.TxSeq
	logSeg := index.PointLogSegment(commit, txSeq, virtualPath)

	// Write the diff file while still holding the lock
	if err := s.writeDiffLocked(virtualPath, commit, txSeq, timestamp, diff, pending); err != nil {
		return 0, 0, err
	}

	// Write sequence state atomically
	if err := s.Seq.WriteStateLocked(state); err != nil {
		return 0, 0, err
	}

	// Write index with seq lock if not pending
	if !pending {
		s.indexMu.Lock()
		s.index.Add(logSeg)
		s.indexMu.Unlock()
	}

	return commit, txSeq, nil
}

// WriteDiff writes a diff file. For atomic sequence allocation and file writing,
// use WriteDiffAtomically instead.
func (s *Storage) WriteDiff(virtualPath string, commitCount, txSeq int64, timestamp string, diff *ir.Node, pending bool) error {
	seg := index.PointLogSegment(commitCount, txSeq, virtualPath)
	err := s.writeDiffLocked(virtualPath, commitCount, txSeq, timestamp, diff, pending)
	if err != nil {
		return err
	}
	if !pending {
		s.indexMu.Lock()
		s.index.Add(seg)
		s.indexMu.Unlock()
	}
	return nil
}

// writeDiffLocked writes a diff file without locking (caller must hold seqMu if needed).
func (s *Storage) writeDiffLocked(virtualPath string, commitCount, txSeq int64, timestamp string, diff *ir.Node, pending bool) error {
	fsPath := s.FS.PathToFilesystem(virtualPath)
	if err := s.FS.EnsurePathDir(virtualPath); err != nil {
		return err
	}
	//
	// Format filename using FS
	seg := index.PointLogSegment(commitCount, txSeq, "")
	filename := paths.FormatLogSegment(seg, 0, pending)
	filePath := filepath.Join(fsPath, filename)

	diffFile := &dfile.DiffFile{
		Seq:       txSeq,
		Path:      virtualPath,
		Timestamp: timestamp,
		Diff:      diff,
	}
	return dfile.WriteDiffFile(filePath, diffFile)
}

// ReadDiff reads a diff file from disk.
// For pending files, commitCount is ignored (can be 0).
// ReadDiff reads a diff file. It acquires its own lock.
func (s *Storage) ReadDiff(virtualPath string, commitCount, txSeq int64, pending bool) (*dfile.DiffFile, error) {
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()
	return s.readDiffLocked(virtualPath, commitCount, txSeq, pending)
}

// readDiffLocked reads a diff file. Caller must hold indexMu (read or write lock).
func (s *Storage) readDiffLocked(virtualPath string, commitCount, txSeq int64, pending bool) (*dfile.DiffFile, error) {
	fsPath := s.FS.PathToFilesystem(virtualPath)

	// Format filename using FS
	seg := index.PointLogSegment(commitCount, txSeq, "")
	filename := paths.FormatLogSegment(seg, 0, pending)
	filePath := filepath.Join(fsPath, filename)
	
	// Read the file - if it doesn't exist, return error immediately
	// The file should exist because commit() renames it before adding to index
	return dfile.ReadDiffFile(filePath)
}
