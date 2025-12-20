package patches

import (
	"strconv"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// PatchIndex maps kinded paths to the dlog entries that affect them.
// Used by StreamingProcessor to identify which subtrees need patching.
type PatchIndex struct {
	byPath map[string][]*dlog.Entry
}

// NewPatchIndex creates an empty PatchIndex.
func NewPatchIndex() *PatchIndex {
	return &PatchIndex{
		byPath: make(map[string][]*dlog.Entry),
	}
}

// BuildPatchIndex walks dlog entries to find nodes tagged with PatchRootTag.
// Returns an index mapping paths to the entries that affect them.
func BuildPatchIndex(entries []*dlog.Entry) *PatchIndex {
	index := NewPatchIndex()

	for _, entry := range entries {
		if entry.Patch == nil {
			continue
		}
		walkIRTree(entry.Patch, "", func(node *ir.Node, path string) {
			if tx.HasPatchRootTag(node) {
				index.byPath[path] = append(index.byPath[path], entry)
			}
		})
	}

	return index
}

// Lookup returns the entries that affect the given path.
func (pi *PatchIndex) Lookup(path string) []*dlog.Entry {
	return pi.byPath[path]
}

// HasPatches returns true if any patches affect the given path.
func (pi *PatchIndex) HasPatches(path string) bool {
	return len(pi.byPath[path]) > 0
}

// Paths returns all paths that have patches.
func (pi *PatchIndex) Paths() []string {
	paths := make([]string, 0, len(pi.byPath))
	for p := range pi.byPath {
		paths = append(paths, p)
	}
	return paths
}

// walkIRTree walks the IR tree depth-first, calling fn for each node with its kinded path.
func walkIRTree(node *ir.Node, path string, fn func(node *ir.Node, path string)) {
	fn(node, path)

	switch node.Type {
	case ir.ObjectType:
		for i, field := range node.Fields {
			var childPath string
			switch field.Type {
			case ir.StringType:
				if path == "" {
					childPath = field.String
				} else {
					childPath = path + "." + field.String
				}
			case ir.NumberType:
				var idx int64
				if field.Int64 != nil {
					idx = *field.Int64
				} else if field.Float64 != nil {
					idx = int64(*field.Float64)
				} else {
					panic("sparse array key has no numeric value")
				}
				childPath = path + "{" + strconv.FormatInt(idx, 10) + "}"
			case ir.NullType:
				panic("merge keys not supported in streaming patch processor")
			}
			walkIRTree(node.Values[i], childPath, fn)
		}
	case ir.ArrayType:
		for i, value := range node.Values {
			childPath := path + "[" + strconv.Itoa(i) + "]"
			walkIRTree(value, childPath, fn)
		}
	}
}
