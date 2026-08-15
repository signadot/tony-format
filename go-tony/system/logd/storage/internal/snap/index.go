package snap

import (
	"io"
	"slices"
	"sync/atomic"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// IndexEntry maps a kinded path to its byte offset in the event stream.
//
// tony:schemagen=index-entry
type IndexEntry struct {
	Path   *Path // Kinded path (e.g., "a.b[0]", "users.123.name")
	Offset int64 // Byte offset in event stream
	Size   int64 `tony:"omit"`
}

// Index maps kinded paths to event stream offsets.
// Entries are in document order (sorted for objects, sequential for arrays).
//
//tony:schemagen=index
type Index struct {
	Entries []IndexEntry // Ordered by Offset

	// nameOrder caches whether Entries are in name order too; see byName.
	nameOrder int32 `tony:"omit"`
}

const (
	nameOrderUnknown int32 = iota
	nameOrderYes
	nameOrderNo
)

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
// when there is none.
//
// Entries are in DOCUMENT order, which is the order of the event stream and the
// only order their offsets are monotone in. The binary search this used to be
// needs them sorted by the comparator it uses, and the one available for paths --
// KPath.Compare -- orders by NAME. logd's own documents satisfy both at once,
// because storage sorts object keys, and that is why this held: it is a
// precondition nothing stated and nothing checked. A snapshot built from a
// document whose fields are in another order made the search wander -- on
// {zz:..., aa:..., mm:...}, Lookup("zz") answered with the entry for the LAST
// field, an offset past everything zz owns, and ReadPath scanned from there to
// the end and found nothing. No error: a path that plainly exists read as absent
// (3cdjz00jh12krns4g1n0).
//
// So the precondition is checked rather than assumed. When it holds the search is
// the same one, tight and logarithmic. When it does not, the answer falls back to
// the deepest ANCESTOR, which is safe with no ordering assumption at all -- a
// value lies inside every ancestor's subtree, so an ancestor's offset is at or
// before it -- and costs only a longer scan.
func (idx *Index) Lookup(kp string) (index int, err error) {
	targetKPath, err := kpath.Parse(kp)
	if err != nil {
		return 0, err
	}
	if !idx.byName() {
		return idx.lookupAncestor(targetKPath), nil
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

// lookupAncestor answers with the last entry that is kp itself or an ancestor of
// it. Every path has one, since the root entry is an ancestor of everything.
func (idx *Index) lookupAncestor(target *kpath.KPath) int {
	best := 0
	for i := range idx.Entries {
		entry := &idx.Entries[i]
		if entry.Path == nil {
			best = i // the root, an ancestor of everything
			continue
		}
		if anc, _ := entry.Path.KPath.AncestorOrEqual(target); anc {
			best = i
		}
	}
	return best
}

// byName reports whether the entries are in name order as well as document
// order, which is what lets Lookup binary search them. Computed once and kept:
// the entries do not change after Open, and a read that recomputed it would pay
// for the scan it is trying to avoid.
func (idx *Index) byName() bool {
	switch atomic.LoadInt32(&idx.nameOrder) {
	case nameOrderYes:
		return true
	case nameOrderNo:
		return false
	}
	state := int32(nameOrderYes)
	if !slices.IsSortedFunc(idx.Entries, compareEntries) {
		state = nameOrderNo
	}
	atomic.StoreInt32(&idx.nameOrder, state)
	return state == nameOrderYes
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
