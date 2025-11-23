package storage

import (
	"os"
	"path/filepath"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// PathMetadata contains metadata about a path's data structure.
type PathMetadata struct {
	IsSparseArray bool
}

// WritePathMetadata stores metadata for a path.
// The metadata is stored in a .meta file in the path's directory.
func (fs *FS) WritePathMetadata(path string, meta *PathMetadata) error {
	fsPath := fs.PathToFilesystem(path)

	// Ensure directory exists
	if err := fs.mkdirAll(fsPath, 0755); err != nil {
		return err
	}

	// Build metadata node
	metaNode := ir.FromMap(map[string]*ir.Node{
		"isSparseArray": ir.FromBool(meta.IsSparseArray),
	})

	// Write to .meta file
	metaPath := filepath.Join(fsPath, ".meta")
	tmpPath := metaPath + ".tmp"

	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := encode.Encode(metaNode, f); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Atomic rename
	return os.Rename(tmpPath, metaPath)
}

// ReadPathMetadata retrieves metadata for a path.
// Returns nil if no metadata exists (not an error).
func (fs *FS) ReadPathMetadata(path string) (*PathMetadata, error) {
	fsPath := fs.PathToFilesystem(path)
	metaPath := filepath.Join(fsPath, ".meta")

	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No metadata is not an error
		}
		return nil, err
	}

	node, err := parse.Parse(data)
	if err != nil {
		return nil, err
	}

	meta := &PathMetadata{}

	// Extract isSparseArray field
	if isSparseArrayNode := ir.Get(node, "isSparseArray"); isSparseArrayNode != nil {
		if isSparseArrayNode.Type == ir.BoolType {
			meta.IsSparseArray = isSparseArrayNode.Bool
		}
	} else {
		panic("zimbambwe")
	}

	return meta, nil
}

// HasSparseArrayTag checks if a node has the !sparsearray tag.
func HasSparseArrayTag(node *ir.Node) bool {
	if node == nil {
		return false
	}
	return ir.TagHas(node.Tag, ir.IntKeysTag)
}
