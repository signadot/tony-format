package snap

import (
	"io"
	"slices"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// IndexEntry maps a kinded path to its byte offset in the event stream.
//
//tony:schemagen=index-entry
type IndexEntry struct {
	Path   *Path // Kinded path (e.g., "a.b[0]", "users.123.name")
	Offset int64 // Byte offset in event stream
	Size   int64 `tony:"omit"`
}

// Index maps kinded paths to event stream offsets.
// Entries are in document order, which is name order too: a snapshot's object keys
// are in name order by contract (directory.go).
//
//tony:schemagen=index
type Index struct {
	Entries []IndexEntry // Ordered by Offset
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

// Lookup finds an index entry a forward scan can reach kp from: the entry whose
// path is greatest among those at or before kp, and the root entry at offset 0
// when there is none. Entries are in document order, which is name order because a
// snapshot's object keys are (directory.go), so a binary search by name finds it.
func (idx *Index) Lookup(kp string) (index int, err error) {
	targetKPath, err := kpath.Parse(kp)
	if err != nil {
		return 0, err
	}

	// Handle root path (empty string) - represented as nil
	var targetEntry IndexEntry
	if targetKPath == nil {
		targetEntry = IndexEntry{Path: nil}
	} else {
		targetEntry = IndexEntry{Path: &Path{*targetKPath}}
	}

	i, found := slices.BinarySearchFunc(idx.Entries, targetEntry, compareEntries)
	if i > 0 && !found {
		// binary search returns insert pos [ 1 2 4 ] looking for 3 would give 2, but we want
		// the one before it unless it was already in there, such as insert pos of 3 in [1 2 3 4] being 2.
		i--
	}
	return i, nil
}

// compareEntries orders index entries by path, the root (a nil path) first.
func compareEntries(a, b IndexEntry) int {
	if a.Path == nil && b.Path == nil {
		return 0
	}
	if a.Path == nil {
		return -1 // a (nil/root) comes before b
	}
	if b.Path == nil {
		return 1 // b (nil/root) comes before a
	}
	return a.Path.KPath.Compare(&b.Path.KPath)
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
