package snap

import (
	"fmt"
	"io"
	"slices"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// IndexEntry represents a single entry in the snapshot index.
// Each entry maps a kinded path to its byte offset in the event stream.
//
// tony:schemagen=index-entry
type IndexEntry struct {
	Path   *Path // Kinded path (e.g., "a.b[0]", "users.123.name")
	Offset int64 // Byte offset in the event stream where this path appears
}

// Index is an index into event-based snapshots.
// It contains a list of kpaths in order of the stream events,
// each associated with an offset in the event data.
//
//tony:schemagen=index
type Index struct {
	// Entries is a list of indexed paths in order of appearance in the event stream.
	// Entries are ordered by their Offset values.
	Entries []IndexEntry
}

// OpenIndex reads an index from a reader of size size
func OpenIndex(r io.Reader, size int) (*Index, error) {
	buf := make([]byte, size)
	_, err := io.ReadFull(r, buf)
	if err != nil {
		return nil, err
	}
	idx := &Index{}
	if err := idx.FromTony(buf); err != nil {
		return nil, err
	}
	return idx, nil
}

// Lookup finds the entry for the given path, or the entry just before
func (idx *Index) Lookup(kp string) (entry *IndexEntry, exact bool, err error) {
	targetKPath, err := kpath.Parse(kp)
	if err != nil {
		return nil, false, err
	}
	if targetKPath == nil {
		// Empty path - would need special handling
		return nil, false, fmt.Errorf("empty path not supported?!?")
	}

	i, found := slices.BinarySearchFunc(idx.Entries, IndexEntry{Path: &Path{*targetKPath}}, func(a, b IndexEntry) int {
		return a.Path.KPath.Compare(&b.Path.KPath)
	})
	if found {
		return &idx.Entries[i], true, nil
	}
	if i != 0 {
		return &idx.Entries[i-1], false, nil
	}
	return nil, false, nil
}

// EstimatedSize returns an estimate of the index size in bytes.
func (idx *Index) EstimatedSize() int64 {
	size := int64(0)
	for _, entry := range idx.Entries {
		if entry.Path != nil {
			size += int64(len(entry.Path.String())) // String length
		}
		size += 8 // Offset (int64)
		size += 8 // Overhead (pointer, etc.)
	}
	return size
}
