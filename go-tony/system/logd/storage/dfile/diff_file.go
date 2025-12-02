package dfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/paths"
)

// DiffFile represents a diff file on disk for the logd storage
// system.
//
//tony:schemagen=diff-file
type DiffFile struct {
	Seq       int64
	Path      string
	Timestamp string
	Diff      *ir.Node
	Pending   bool // true for .pending files, false for .diff files

	// compaction metadata
	Inputs int
}

func ReadDiffFile(p string) (*DiffFile, error) {
	d, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	res := &DiffFile{}
	if err := res.FromTony(d); err != nil {
		return nil, err
	}
	return res, nil
}

// WriteDiffFile writes df to p.tmp and then
// renames it to p.
func WriteDiffFile(p string, df *DiffFile) error {
	d, err := df.ToTony()
	if err != nil {
		return err
	}
	// Write to temp file first, then rename atomically
	tmpFile := p + ".tmp"
	if err := os.WriteFile(tmpFile, d, 0644); err != nil {
		return err
	}

	// Atomic rename
	if err := os.Rename(tmpFile, p); err != nil {
		os.Remove(tmpFile) // Clean up on error
		return err
	}
	return nil
}

// CommitPending atomically commits a pending diff file by renaming it
// with the given commit count.
func CommitPending(dir string, seg *index.LogSegment, level int, commitCount int64) error {
	// Build filenames using helper methods
	// FormatLogSegment includes RelPath in the name, but dir already points to that directory
	// So we need to extract just the base filename
	oldFormatted := paths.FormatLogSegment(seg.AsPending(), level, true)
	newFormatted := paths.FormatLogSegment(seg.WithCommit(commitCount), level, false)

	// Extract just the filename (last component after the path separator)
	_, oldName := filepath.Split(oldFormatted)
	_, newName := filepath.Split(newFormatted)
	if oldName == "" {
		oldName = oldFormatted
	}
	if newName == "" {
		newName = newFormatted
	}

	// Atomic rename
	oldPath := filepath.Join(dir, oldName)
	newPath := filepath.Join(dir, newName)

	// Hypothesis #2: Verify filename construction matches
	// Log the filenames being used for debugging
	// Check if oldPath matches what was written (will be checked by caller)

	// Verify the pending file exists before trying to rename it
	// This helps catch bugs where the file was never created or was deleted
	if _, err := os.Stat(oldPath); err != nil {
		// List directory contents to help debug filename mismatch
		dirEnts, listErr := os.ReadDir(dir)
		dirContents := []string{}
		if listErr == nil {
			for _, de := range dirEnts {
				if !de.IsDir() && (strings.HasSuffix(de.Name(), ".pending") || strings.HasSuffix(de.Name(), ".diff")) {
					dirContents = append(dirContents, de.Name())
				}
			}
		}
		return fmt.Errorf("pending file does not exist (cannot rename %q to %q): %w (directory contents: %v)", oldPath, newPath, err, dirContents)
	}

	return os.Rename(oldPath, newPath)
}

// DeletePending removes a pending diff file.
func DeletePending(dir string, seg *index.LogSegment, level int) error {
	filename := paths.FormatLogSegment(seg.AsPending(), level, true)
	return os.Remove(filepath.Join(dir, filename))
}
