package storage

import (
	"os"
	"path/filepath"
	"strings"
)

// PathToFilesystem converts a virtual document path to a filesystem directory path.
// Example: "/proc/processes" -> "/logd/paths/children/proc/children/processes"
func (s *Storage) PathToFilesystem(virtualPath string) string {
	// Remove leading slash if present
	path := virtualPath
	if len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}

	if path == "" {
		return filepath.Join(s.Root, "paths")
	}

	// Split into segments
	segments := strings.Split(path, "/")

	// Build path with "children" interleaved
	// root/paths/children/seg1/children/seg2...
	parts := make([]string, 0, 2+len(segments)*2)
	parts = append(parts, s.Root, "paths")
	for _, seg := range segments {
		parts = append(parts, "children", seg)
	}
	return filepath.Join(parts...)
}

// FilesystemToPath converts a filesystem directory path back to a virtual document path.
// Example: "/logd/paths/children/proc/children/processes" -> "/proc/processes"
func (s *Storage) FilesystemToPath(fsPath string) string {
	prefix := filepath.Join(s.Root, "paths")
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

// EnsurePathDir ensures the directory for a virtual path exists.
func (s *Storage) EnsurePathDir(virtualPath string) error {
	fsPath := s.PathToFilesystem(virtualPath)
	return s.mkdirAll(fsPath, 0755)
}

// ListChildPaths returns all immediate child paths under parentPath.
// Only returns paths that have data (directories with diff files).
func (s *Storage) ListChildPaths(parentPath string) ([]string, error) {
	fsPath := s.PathToFilesystem(parentPath)

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
