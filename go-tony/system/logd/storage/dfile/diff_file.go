package dfile

import (
	"os"
	"path/filepath"

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
	oldName := paths.FormatLogSegment(seg.AsPending(), level, true)
	newName := paths.FormatLogSegment(seg.WithCommit(commitCount), level, false)

	// Atomic rename
	oldPath := filepath.Join(dir, oldName)
	newPath := filepath.Join(dir, newName)

	return os.Rename(oldPath, newPath)
}

// DeletePending removes a pending diff file.
func DeletePending(dir string, seg *index.LogSegment, level int) error {
	filename := paths.FormatLogSegment(seg.AsPending(), level, true)
	return os.Remove(filepath.Join(dir, filename))
}
