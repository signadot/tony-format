package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

// DiffFile represents a diff file on disk.
type DiffFile struct {
	Seq       int64
	Path      string
	Timestamp string
	Diff      *ir.Node
	Pending   bool // true for .pending files, false for .diff files
}

// WriteDiffAtomically writes a diff file to disk.
// commitCount should be 0 for pending files, and the actual commit count for committed files.
// If pending is true, writes as .pending file; otherwise writes as .diff file.
// WriteDiffAtomically atomically allocates sequence numbers and writes the diff file.
// This ensures that files are written in the order that sequence numbers are allocated,
// preventing race conditions where a later sequence number is written before an earlier one.
func (s *Storage) WriteDiffAtomically(virtualPath string, timestamp string, diff *ir.Node, pending bool) (commitCount, txSeq int64, err error) {
	s.Seq.Lock()
	defer s.Seq.Unlock()

	// Get current state and increment both sequence numbers
	state, err := s.Seq.ReadStateLocked()
	if err != nil {
		return 0, 0, err
	}

	state.CommitCount++
	state.TxSeq++

	commitCount = state.CommitCount
	txSeq = state.TxSeq

	// Write the diff file while still holding the lock
	if err := s.writeDiffLocked(virtualPath, commitCount, txSeq, timestamp, diff, pending); err != nil {
		return 0, 0, err
	}

	// Write sequence state atomically
	if err := s.Seq.WriteStateLocked(state); err != nil {
		return 0, 0, err
	}

	// Check if diff has !sparsearray tag and store metadata
	// This is done after the diff is written to avoid blocking on metadata writes
	if HasSparseArrayTag(diff) {
		meta := &PathMetadata{IsSparseArray: true}
		if err := s.FS.WritePathMetadata(virtualPath, meta); err != nil {
			// Log but don't fail the write - metadata is optional
			s.logger.Warn("failed to write path metadata", "path", virtualPath, "error", err)
		}
	}
	// Write index with seq lock
	//
	s.index.Add(index.PointLogSegment(state.CommitCount, state.TxSeq, virtualPath))

	return commitCount, txSeq, nil
}

// WriteDiff writes a diff file. For atomic sequence allocation and file writing,
// use WriteDiffAtomically instead.
func (s *Storage) WriteDiff(virtualPath string, commitCount, txSeq int64, timestamp string, diff *ir.Node, pending bool) error {
	return s.writeDiffLocked(virtualPath, commitCount, txSeq, timestamp, diff, pending)
}

// writeDiffLocked writes a diff file without locking (caller must hold seqMu if needed).
func (s *Storage) writeDiffLocked(virtualPath string, commitCount, txSeq int64, timestamp string, diff *ir.Node, pending bool) error {
	fsPath := s.FS.PathToFilesystem(virtualPath)
	if err := s.FS.EnsurePathDir(virtualPath); err != nil {
		return err
	}

	// Format filename using FS
	seg := index.PointLogSegment(commitCount, txSeq, "")
	filename := s.FS.FormatLogSegment(seg, pending)
	filePath := filepath.Join(fsPath, filename)

	// Create the diff file structure using FromMap to preserve parent pointers
	seqNode := &ir.Node{Type: ir.NumberType, Int64: &txSeq, Number: strconv.FormatInt(txSeq, 10)}
	diffFile := ir.FromMap(map[string]*ir.Node{
		"seq":       seqNode,
		"timestamp": &ir.Node{Type: ir.StringType, String: timestamp},
		"path":      &ir.Node{Type: ir.StringType, String: virtualPath},
		"diff":      diff,
	})

	// Encode to Tony format
	var buf strings.Builder
	if err := encode.Encode(diffFile, &buf); err != nil {
		return fmt.Errorf("failed to encode diff file: %w", err)
	}

	// Write to temp file first, then rename atomically
	tmpFile := filePath + ".tmp"
	if err := os.WriteFile(tmpFile, []byte(buf.String()), 0644); err != nil {
		return err
	}

	// Atomic rename
	if err := os.Rename(tmpFile, filePath); err != nil {
		os.Remove(tmpFile) // Clean up on error
		return err
	}

	return nil
}

// ReadDiff reads a diff file from disk.
// For pending files, commitCount is ignored (can be 0).
func (s *Storage) ReadDiff(virtualPath string, commitCount, txSeq int64, pending bool) (*DiffFile, error) {
	fsPath := s.FS.PathToFilesystem(virtualPath)

	// Format filename using FS
	seg := index.PointLogSegment(commitCount, txSeq, "")
	filename := s.FS.FormatLogSegment(seg, pending)
	filePath := filepath.Join(fsPath, filename)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Parse Tony document
	node, err := parse.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse diff file: %w", err)
	}

	// Extract fields from the diff file structure
	if node.Type != ir.ObjectType {
		return nil, fmt.Errorf("expected object, got %v", node.Type)
	}

	var timestamp, path string
	var diff *ir.Node

	for i, field := range node.Fields {
		if i >= len(node.Values) {
			break
		}
		value := node.Values[i]

		switch field.String {
		case "seq":
			// Ignore seq from file content, we use txSeq from filename
		case "timestamp":
			if value.Type == ir.StringType {
				timestamp = value.String
			}
		case "path":
			if value.Type == ir.StringType {
				path = value.String
			}
		case "diff":
			diff = value
		}
	}

	if diff == nil {
		return nil, fmt.Errorf("missing diff field in diff file")
	}

	return &DiffFile{
		Seq:       txSeq, // Use txSeq from filename, not from file content
		Path:      path,
		Timestamp: timestamp,
		Diff:      diff,
		Pending:   pending,
	}, nil
}
