package storage

import (
	"sort"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// indexPaths lists every path the index holds a segment at, deduplicated.
func indexPaths(s *Storage) []string {
	seen := map[string]bool{}
	for _, seg := range s.index.AllSegments() {
		seen[seg.KindedPath] = true
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// TestKey_IndexPathShapeVariesPerWrite shows that whether an array is "keyed" is
// decided per WRITE, from the tag that write happens to carry. The same array ends up
// indexed under both keyed and positional paths depending on which writes tagged it.
//
// This matters for the planned scope overlay, which is keyed BY INDEX PATH: if the same
// logical element is reachable as items("a") from one commit and items[0] from another,
// a latest-per-path materialization has two different keys for one element.
func TestKey_IndexPathShapeVariesPerWrite(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	scalingCommit(t, s, nil, `{items: !key(name) [{name: "a", v: 1}]}`, nil)
	t.Logf("after tagged write:   %v", filterItems(indexPaths(s)))

	scalingCommit(t, s, nil, `{items: [{name: "a", v: 2}]}`, nil)
	t.Logf("after untagged write: %v", filterItems(indexPaths(s)))
}

// TestKey_SchemaRouteIsAutoIDOnly shows the schema route only covers auto-generated
// keys, and that the write it produces is not navigable by the index path it records.
func TestKey_SchemaRouteIsAutoIDOnly(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// Declare items as auto-id keyed by "id" — the ONLY thing a schema can say about
	// keying (Schema holds AutoIDFields and nothing else).
	s.SetSchemaResolver(&api.StaticSchemaResolver{Schema: &api.Schema{
		AutoIDFields: []api.AutoIDField{{Path: "items", Field: "id"}},
	}})

	scalingCommit(t, s, nil, `{items: [{v: 1}]}`, nil)
	paths := filterItems(indexPaths(s))
	t.Logf("schema-keyed write, index paths: %v", paths)

	doc := showDoc(t, s, nil, "state")
	t.Logf("the id was injected into the data (%s) but no !key tag rides with it,", doc)
	t.Logf("so the recorded index path is not resolvable from the entry's own patch.")

	// And a client-supplied key like !key(name) has NO schema route at all: Schema can
	// only express auto-id fields, so "items is keyed by name" is unsayable in schema
	// and can only ever be carried per-write as a tag.
}

func filterItems(paths []string) []string {
	var out []string
	for _, p := range paths {
		if strings.HasPrefix(p, "items") {
			out = append(out, p)
		}
	}
	return out
}
