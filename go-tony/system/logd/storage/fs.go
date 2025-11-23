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

func (fs *FS) MetaPath() string {
	return filepath.Join(fs.Root, "meta")
}

func (fs *FS) FormatLogSegment(s *index.LogSegment, pending bool) string {
	if s.IsPoint() {
		base := fmt.Sprintf("%s-%s.%s", FormatLexInt(s.StartCommit), FormatLexInt(s.StartTx), segExt(pending))
		return path.Join(s.RelPath, base)
	}
	base := fmt.Sprintf("%s.%s-%s.%s.%s",
		FormatLexInt(s.StartCommit),
		FormatLexInt(s.StartTx),
		FormatLexInt(s.EndCommit),
		FormatLexInt(s.EndTx),
		segExt(pending),
	)
	return path.Join(s.RelPath, base)
}

func (fs *FS) ParseLogSegment(p string) (*index.LogSegment, error) {
	dir, base := path.Split(p)
	switch path.Ext(base) {
	case ".diff":
		base = strings.TrimSuffix(base, ".diff")
	case ".pending":
		base = strings.TrimSuffix(base, ".pending")
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
	seg := index.PointLogSegment(0, txSeq, virtualPath)
	oldFile := fs.FormatLogSegment(seg, true)
	seg.StartCommit = newCommitCount
	newFile := fs.FormatLogSegment(seg, false)

	oldFile = fs.PathToFilesystem(oldFile)
	newFile = fs.PathToFilesystem(newFile)

	// Atomic rename
	if err := os.Rename(oldFile, newFile); err != nil {
		return err
	}
	return nil
}

// DeletePending deletes a .pending file.
func (fs *FS) DeletePending(virtualPath string, txSeq int64) error {
	fsPath := fs.PathToFilesystem(virtualPath)
	filename := fs.FormatLogSegment(index.PointLogSegment(0, txSeq, virtualPath), true)
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

	if len(virtualSegments) == 0 {
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

// ListLogSegments lists all committed diff files for a path, ordered by commit count.
// Only returns .diff files, not .pending files.
func (fs *FS) ListLogSegments(virtualPath string) ([]*index.LogSegment, error) {
	fsPath := fs.PathToFilesystem(virtualPath)

	entries, err := os.ReadDir(fsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var segments []*index.LogSegment
	for _, entry := range entries {
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

// EnsurePathDir ensures the directory for a virtual path exists.
func (fs *FS) EnsurePathDir(virtualPath string) error {
	fsPath := fs.PathToFilesystem(virtualPath)
	return fs.mkdirAll(fsPath, 0755)
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

		// Skip metadata directory if it exists (though it shouldn't be in children dir)
		if entry.Name() == ".meta" {
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

// mkdirAll is a helper wrapper around os.MkdirAll
func (fs *FS) mkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}
