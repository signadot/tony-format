package storage

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/paths"
)

type FS struct {
	Root string
}

// PathToFilesystem converts a virtual document path to a filesystem directory path.
// Example: "/proc/processes" -> "/logd/paths/children/proc/children/processes"
func (fs *FS) PathToFilesystem(virtualPath string) string {
	return paths.PathToFilesystem(fs.Root, virtualPath)
}

// FilesystemToPath converts a filesystem directory path back to a virtual document path.
// Example: "/logd/paths/children/proc/children/processes" -> "/proc/processes"
func (fs *FS) FilesystemToPath(fsPath string) string {
	return paths.FilesystemToPath(fs.Root, fsPath)
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

		seg, _, err := paths.ParseLogSegment(filepath.Join(virtualPath, name))
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
