package storage

import (
	"path/filepath"
)

// PathToFilesystem converts a virtual document path to a filesystem directory path.
// Example: "/proc/processes" -> "/logd/paths/proc/processes"
func (s *Storage) PathToFilesystem(virtualPath string) string {
	// Remove leading slash if present, then join with paths directory
	path := virtualPath
	if len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}
	return filepath.Join(s.root, "paths", path)
}

// FilesystemToPath converts a filesystem directory path back to a virtual document path.
// Example: "/logd/paths/proc/processes" -> "/proc/processes"
func (s *Storage) FilesystemToPath(fsPath string) string {
	prefix := filepath.Join(s.root, "paths")
	rel, err := filepath.Rel(prefix, fsPath)
	if err != nil {
		return ""
	}
	// Convert to forward slashes and add leading slash
	return "/" + filepath.ToSlash(rel)
}

// EnsurePathDir ensures the directory for a virtual path exists.
func (s *Storage) EnsurePathDir(virtualPath string) error {
	fsPath := s.PathToFilesystem(virtualPath)
	return s.mkdirAll(fsPath, 0755)
}
