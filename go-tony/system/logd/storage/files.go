package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// formatDiffFilename formats a diff filename.
// For pending files (ext="pending"): {txSeq}.pending
// For committed files (ext="diff"): {commitCount}-{txSeq}.diff
func formatDiffFilename(commitCount, txSeq int64, ext string) string {
	if ext == "pending" {
		// Pending files don't have commit count prefix
		return fmt.Sprintf("%d.%s", txSeq, ext)
	}
	// Committed files have commit count prefix
	return fmt.Sprintf("%d-%d.%s", commitCount, txSeq, ext)
}

// parseDiffFilename parses a diff filename to extract commitCount and txSeq.
// Handles both formats: {txSeq}.pending and {commitCount}-{txSeq}.diff
func parseDiffFilename(filename string) (commitCount, txSeq int64, ext string, err error) {
	// Remove extension
	parts := strings.Split(filename, ".")
	if len(parts) != 2 {
		return 0, 0, "", fmt.Errorf("invalid filename format: expected 'txSeq.pending' or 'commitCount-txSeq.diff'")
	}
	ext = parts[1]
	
	// Check if it's a pending file (no commit count prefix)
	if ext == "pending" {
		txSeq, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, 0, "", fmt.Errorf("invalid transaction seq: %w", err)
		}
		return 0, txSeq, ext, nil
	}
	
	// For .diff files, split commitCount and txSeq
	seqParts := strings.Split(parts[0], "-")
	if len(seqParts) != 2 {
		return 0, 0, "", fmt.Errorf("invalid filename format: expected 'commitCount-txSeq.diff'")
	}
	
	commitCount, err = strconv.ParseInt(seqParts[0], 10, 64)
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid commit count: %w", err)
	}
	
	txSeq, err = strconv.ParseInt(seqParts[1], 10, 64)
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid transaction seq: %w", err)
	}
	
	return commitCount, txSeq, ext, nil
}

// DiffFile represents a diff file on disk.
type DiffFile struct {
	Seq       int64
	Path      string
	Timestamp string
	Diff      *ir.Node
	Pending   bool // true for .pending files, false for .diff files
}

// WriteDiff writes a diff file to disk.
// commitCount should be 0 for pending files, and the actual commit count for committed files.
// If pending is true, writes as .pending file; otherwise writes as .diff file.
// WriteDiffAtomically atomically allocates sequence numbers and writes the diff file.
// This ensures that files are written in the order that sequence numbers are allocated,
// preventing race conditions where a later sequence number is written before an earlier one.
func (s *Storage) WriteDiffAtomically(virtualPath string, timestamp string, diff *ir.Node, pending bool) (commitCount, txSeq int64, err error) {
	s.seqMu.Lock()
	defer s.seqMu.Unlock()
	
	// Get current state and increment both sequence numbers
	state, err := s.readSeqStateLocked()
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
	if err := s.writeSeqStateLocked(state); err != nil {
		return 0, 0, err
	}
	
	return commitCount, txSeq, nil
}

// WriteDiff writes a diff file. For atomic sequence allocation and file writing,
// use WriteDiffAtomically instead.
func (s *Storage) WriteDiff(virtualPath string, commitCount, txSeq int64, timestamp string, diff *ir.Node, pending bool) error {
	return s.writeDiffLocked(virtualPath, commitCount, txSeq, timestamp, diff, pending)
}

// writeDiffLocked writes a diff file without locking (caller must hold seqMu if needed).
func (s *Storage) writeDiffLocked(virtualPath string, commitCount, txSeq int64, timestamp string, diff *ir.Node, pending bool) error {
	fsPath := s.PathToFilesystem(virtualPath)
	if err := s.EnsurePathDir(virtualPath); err != nil {
		return err
	}

	// Format filename: {commitCount}-{txSeq}.{ext}
	ext := "diff"
	if pending {
		ext = "pending"
	}
	filename := formatDiffFilename(commitCount, txSeq, ext)
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
	fsPath := s.PathToFilesystem(virtualPath)

	// Format filename: {txSeq}.pending or {commitCount}-{txSeq}.diff
	ext := "diff"
	if pending {
		ext = "pending"
	}
	filename := formatDiffFilename(commitCount, txSeq, ext)
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

// ListDiffs lists all committed diff files for a path, ordered by commit count.
// Only returns .diff files, not .pending files.
// Returns a slice of (commitCount, txSeq) pairs.
func (s *Storage) ListDiffs(virtualPath string) ([]struct{ CommitCount, TxSeq int64 }, error) {
	fsPath := s.PathToFilesystem(virtualPath)

	entries, err := os.ReadDir(fsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var diffs []struct{ CommitCount, TxSeq int64 }
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".diff") {
			continue
		}

		// Parse filename: {commitCount}-{txSeq}.diff
		commitCount, txSeq, ext, err := parseDiffFilename(name)
		if err != nil {
			s.logger.Warn("skipping invalid diff filename", "filename", name, "error", err)
			continue
		}
		if ext != "diff" {
			continue
		}

		diffs = append(diffs, struct{ CommitCount, TxSeq int64 }{commitCount, txSeq})
	}

	// Sort by commit count (monotonic)
	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].CommitCount < diffs[j].CommitCount
	})

	return diffs, nil
}

// RenamePendingToDiff atomically renames a .pending file to .diff file with new commit count.
func (s *Storage) RenamePendingToDiff(virtualPath string, newCommitCount, txSeq int64) error {
	fsPath := s.PathToFilesystem(virtualPath)

	oldFilename := formatDiffFilename(0, txSeq, "pending") // Pending files don't use commit count
	newFilename := formatDiffFilename(newCommitCount, txSeq, "diff")
	
	pendingFile := filepath.Join(fsPath, oldFilename)
	diffFile := filepath.Join(fsPath, newFilename)

	// Atomic rename
	if err := os.Rename(pendingFile, diffFile); err != nil {
		return err
	}

	return nil
}

// DeletePending deletes a .pending file.
func (s *Storage) DeletePending(virtualPath string, txSeq int64) error {
	fsPath := s.PathToFilesystem(virtualPath)
	filename := formatDiffFilename(0, txSeq, "pending") // Pending files don't use commit count
	pendingFile := filepath.Join(fsPath, filename)
	return os.Remove(pendingFile)
}
