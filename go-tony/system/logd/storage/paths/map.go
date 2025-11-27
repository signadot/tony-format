package paths

import (
	"path/filepath"
	"strings"
)

// PathToFilesystem converts a virtual document path to a filesystem directory path.
// Example: "/proc/processes" -> "/logd/paths/children/proc/children/processes"
func PathToFilesystem(fsRoot string, virtualPath string) string {
	// Remove leading slash if present
	path := virtualPath
	if len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}

	if path == "" {
		return filepath.Join(fsRoot, "paths")
	}

	// Split into segments
	segments := strings.Split(path, "/")

	// Build path with "children" interleaved
	// root/paths/children/seg1/children/seg2...
	parts := make([]string, 0, 2+len(segments)*2)
	parts = append(parts, fsRoot, "paths")
	for _, seg := range segments {
		parts = append(parts, "children", seg)
	}
	return filepath.Join(parts...)
}

// FilesystemToPath converts a filesystem directory path back to a virtual document path.
// Example: "/logd/paths/children/proc/children/processes" -> "/proc/processes"
func FilesystemToPath(fsRoot, fsPath string) string {
	prefix := filepath.Join(fsRoot, "paths")
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
