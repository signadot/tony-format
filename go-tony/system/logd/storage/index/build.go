package index

import (
	"io/fs"
	"path/filepath"
)

// Build reconstructs the index from the filesystem rooted at root.
// extract is a closure that takes a filesystem path and returns a LogSegment if the file represents one.
// If extract returns nil, the file is ignored.
func Build(root string, extract func(path string) (*LogSegment, error)) (*Index, error) {
	idx := NewIndex("")

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		seg, err := extract(path)
		if err != nil {
			return err
		}
		if seg != nil {
			idx.Add(seg)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return idx, nil
}
