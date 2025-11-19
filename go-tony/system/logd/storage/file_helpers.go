package storage

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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
