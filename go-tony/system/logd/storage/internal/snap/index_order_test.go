package snap

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
)

// TestReadPathWhenFieldsAreNotInNameOrder: Lookup binary searched entries held in
// DOCUMENT order using a comparator that orders by NAME. logd's own snapshots
// satisfy both, because storage sorts object keys, so the precondition held
// without ever being stated. A snapshot of a document in any other field order
// made the search wander past the target, and ReadPath scanned from an offset
// after everything it was looking for and answered with nothing -- no error, a
// path that exists reading as absent.
//
// No comments anywhere in this test: the bug is the index's, and it was found
// while building the comment reads (3cdjz00jh12krns4g1n0).
func TestReadPathWhenFieldsAreNotInNameOrder(t *testing.T) {
	const doc = `zz:
  b: 1
  c: 2
aa: 3
mm:
- 4
- 5
`
	// One event per chunk, so every path has an index entry of its own and the
	// search has somewhere wrong to land.
	t.Setenv("SNAP_MAX_CHUNK_SIZE", "1")
	s, _ := snapOf(t, doc)

	want, err := parse.Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"zz", "zz.b", "zz.c", "aa", "mm", "mm[0]", "mm[1]"} {
		got, err := s.ReadPath(p)
		if err != nil {
			t.Fatalf("ReadPath(%q): %v", p, err)
		}
		exp, err := want.GetKPath(p)
		if err != nil {
			t.Fatal(err)
		}
		if got == nil {
			t.Errorf("read %q as nothing; the document has %s", p, show(t, exp))
			continue
		}
		if !got.DeepEqual(exp) {
			t.Errorf("read %q as %s and the document has %s", p, show(t, got), show(t, exp))
		}
	}
}

// TestLookupFallsBackToAnAncestor: when the entries are not in name order the
// answer is the deepest ancestor, which is safe whatever the document order is.
func TestLookupFallsBackToAnAncestor(t *testing.T) {
	idx := &Index{Entries: []IndexEntry{
		{Path: nil, Offset: 0},
		{Path: mustParsePath(t, "zz"), Offset: 10},
		{Path: mustParsePath(t, "zz.b"), Offset: 20},
		{Path: mustParsePath(t, "aa"), Offset: 30},
	}}
	for _, tc := range []struct {
		path string
		want int64
	}{
		{"zz", 10},
		{"zz.b", 20},
		{"zz.c", 10}, // no entry of its own: its parent
		{"aa", 30},   // an exact entry is still exact
		{"qq", 0},    // nothing knows where it is: the root
		{"aa.deep", 30},
	} {
		i, err := idx.Lookup(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if got := idx.Entries[i].Offset; got != tc.want {
			t.Errorf("Lookup(%q) -> offset %d, want %d", tc.path, got, tc.want)
		}
	}
}

// TestSnapshotEventsAreDocumentOrder guards the assumption the fallback rests on:
// an entry's offset is where its path's events begin, so an ancestor's offset is
// at or before its descendant's.
func TestSnapshotEventsAreDocumentOrder(t *testing.T) {
	const doc = "zz:\n  b: 1\naa: 2\n"
	t.Setenv("SNAP_MAX_CHUNK_SIZE", "1")
	s, _ := snapOf(t, doc)
	var last int64 = -1
	for _, e := range s.Index.Entries {
		if e.Offset < last {
			t.Fatalf("entry %q at offset %d follows one at %d", e.Path.String(), e.Offset, last)
		}
		last = e.Offset
	}
}
