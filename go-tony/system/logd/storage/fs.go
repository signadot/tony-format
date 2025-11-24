package storage

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

type FS struct {
	Root string
}

func (fs *FS) FormatLogSegment(s *index.LogSegment, pending bool) string {
	var base string
	if s.IsPoint() {
		if pending {
			// Pending files: just tx
			base = fmt.Sprintf("%s.%s", FormatLexInt(s.StartTx), segExt(pending))
		} else {
			// Committed point: commit-tx
			base = fmt.Sprintf("%s-%s.%s", FormatLexInt(s.StartCommit), FormatLexInt(s.StartTx), segExt(pending))
		}
	} else {
		// Compacted: commit.tx-commit.tx
		base = fmt.Sprintf("%s.%s-%s.%s.%s",
			FormatLexInt(s.StartCommit),
			FormatLexInt(s.StartTx),
			FormatLexInt(s.EndCommit),
			FormatLexInt(s.EndTx),
			segExt(pending),
		)
	}
	return path.Join(s.RelPath, base)
}

func (fs *FS) ParseLogSegment(p string) (*index.LogSegment, error) {
	dir, base := path.Split(p)
	// Trim trailing slash from dir
	dir = strings.TrimSuffix(dir, "/")

	ext := path.Ext(base)
	switch ext {
	case ".diff":
		base = strings.TrimSuffix(base, ".diff")
	case ".pending":
		base = strings.TrimSuffix(base, ".pending")
		// Pending files: just tx
		tx, err := ParseLexInt(base)
		if err != nil {
			return nil, err
		}
		return index.PointLogSegment(0, tx, dir), nil
	default:
		return nil, fmt.Errorf("unrecognized ext %q", path.Ext(base))
	}

	parts := strings.Split(base, "-")
	if len(parts) != 2 {
		return nil, fmt.Errorf("unrecognized base format %q", base)
	}
	b4, after := parts[0], parts[1]
	if strings.Contains(b4, ".") {
		res := &index.LogSegment{RelPath: dir}
		// compacated diff
		parts := strings.Split(b4, ".")
		if len(parts) != 2 {
			return nil, fmt.Errorf("unrecognized base format %q", base)
		}
		startCom, startT, err := parseCommitTx(parts[0], parts[1])
		if err != nil {
			return nil, err
		}
		res.StartCommit = startCom
		res.StartTx = startT
		parts = strings.Split(after, ".")
		if len(parts) != 2 {
			return nil, fmt.Errorf("unrecognized base format %q", base)
		}
		endCom, endT, err := parseCommitTx(parts[0], parts[1])
		if err != nil {
			return nil, err
		}
		res.EndCommit = endCom
		res.EndTx = endT
		return res, nil
	}
	commit, tx, err := parseCommitTx(b4, after)
	if err != nil {
		return nil, err
	}
	return index.PointLogSegment(commit, tx, dir), nil
}

func (fs *FS) RenamePendingToDiff(virtualPath string, newCommitCount, txSeq int64) error {
	// Format filenames with empty RelPath (just the basename)
	seg := index.PointLogSegment(0, txSeq, "")
	oldFilename := fs.FormatLogSegment(seg, true)

	seg.StartCommit = newCommitCount
	seg.EndCommit = seg.StartCommit
	newFilename := fs.FormatLogSegment(seg, false)

	// Get filesystem directory path
	fsPath := fs.PathToFilesystem(virtualPath)

	// Build full paths
	oldFile := filepath.Join(fsPath, oldFilename)
	newFile := filepath.Join(fsPath, newFilename)
	//fmt.Printf("renaming %s -> %s\n", oldFile, newFile)

	// Atomic rename
	if err := os.Rename(oldFile, newFile); err != nil {
		return err
	}
	return nil
}

// DeletePending deletes a .pending file.
func (fs *FS) DeletePending(virtualPath string, txSeq int64) error {
	fsPath := fs.PathToFilesystem(virtualPath)
	filename := fs.FormatLogSegment(index.PointLogSegment(0, txSeq, ""), true)
	pendingFile := filepath.Join(fsPath, filename)
	return os.Remove(pendingFile)
}

// PathToFilesystem converts a virtual document path to a filesystem directory path.
// Example: "/proc/processes" -> "/logd/paths/children/proc/children/processes"
func (fs *FS) PathToFilesystem(virtualPath string) string {
	// Remove leading slash if present
	path := virtualPath
	if len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}

	if path == "" {
		return filepath.Join(fs.Root, "paths")
	}

	// Split into segments
	segments := strings.Split(path, "/")

	// Build path with "children" interleaved
	// root/paths/children/seg1/children/seg2...
	parts := make([]string, 0, 2+len(segments)*2)
	parts = append(parts, fs.Root, "paths")
	for _, seg := range segments {
		parts = append(parts, "children", seg)
	}
	return filepath.Join(parts...)
}

// FilesystemToPath converts a filesystem directory path back to a virtual document path.
// Example: "/logd/paths/children/proc/children/processes" -> "/proc/processes"
func (fs *FS) FilesystemToPath(fsPath string) string {
	prefix := filepath.Join(fs.Root, "paths")
	rel, err := filepath.Rel(prefix, fsPath)
	if err != nil {
		return ""
	}

	// Split into segments
	segments := strings.Split(rel, string(filepath.Separator))

	// Filter out "children" segments
	var virtualSegments []string
	for _, seg := range segments {
		if seg == "children" {
			continue
		}
		virtualSegments = append(virtualSegments, seg)
	}

	if len(virtualSegments) == 0 || (len(virtualSegments) == 1 && virtualSegments[0] == ".") {
		return "/"
	}

	// Join with forward slashes and add leading slash
	return "/" + strings.Join(virtualSegments, "/")
}

func parseCommitTx(c, t string) (commit, tx int64, err error) {
	commit, err = ParseLexInt(c)
	if err != nil {
		return 0, 0, err
	}
	tx, err = ParseLexInt(t)
	if err != nil {
		return 0, 0, err
	}
	return
}

func segExt(pending bool) string {
	if pending {
		return "pending"
	}
	return "diff"
}

// ListLogSegments lists all diff and pending files for a path, ordered by commit count then tx.
func (fs *FS) ListLogSegments(virtualPath string) ([]*index.LogSegment, error) {
	//fmt.Printf("list log segments %q\n", virtualPath)
	fsPath := fs.PathToFilesystem(virtualPath)

	entries, err := os.ReadDir(fsPath)
	if err != nil {
		if os.IsNotExist(err) {
			//fmt.Printf("list log segment is not exist!\n")
			return nil, nil
		}
		return nil, err
	}
	//fmt.Printf("%d entries\n", len(entries))

	var segments []*index.LogSegment
	for _, entry := range entries {
		//fmt.Printf("entry: %q\n", entry.Name())
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".diff") && !strings.HasSuffix(name, ".pending") {
			continue
		}

		seg, err := fs.ParseLogSegment(filepath.Join(virtualPath, name))
		if err != nil {
			// Skip invalid files
			continue
		}

		segments = append(segments, seg)
	}

	// Sort by commit count, then tx
	index.SortLogSegments(segments)

	return segments, nil
}

// EnsurePathDir ensures that the directory for a given virtual path exists.
// It creates all necessary parent directories.
func (fs *FS) EnsurePathDir(virtualPath string) error {
	fsPath := fs.PathToFilesystem(virtualPath)
	return os.MkdirAll(fsPath, 0755)
}

// ListChildPaths returns all immediate child paths under parentPath.
// Only returns paths that have data (directories with diff files).
func (fs *FS) ListChildPaths(parentPath string) ([]string, error) {
	fsPath := fs.PathToFilesystem(parentPath)

	// Children are in the "children" subdirectory
	childrenDir := filepath.Join(fsPath, "children")

	entries, err := os.ReadDir(childrenDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var children []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Build child path
		childPath := filepath.Join(parentPath, entry.Name())
		// Ensure forward slashes for virtual path
		childPath = filepath.ToSlash(childPath)
		if !strings.HasPrefix(childPath, "/") {
			childPath = "/" + childPath
		}
		children = append(children, childPath)
	}

	return children, nil
}
