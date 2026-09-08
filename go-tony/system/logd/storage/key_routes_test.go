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

// There is one route to an element's path: the schema declares the identity, lowering
// names the element, and the index records the name as a field. A write that spells !key
// itself is indexed the same way, and one that does not is too.
func TestKey_OneRouteToAnElementsPath(t *testing.T) {
	s := openTestStorage(t)
	declareKeyed(t, s, `{define: {items: {name: !logd-key null}}}`)

	scalingCommit(t, s, nil, `{items: !key(name) [{name: "a", v: 1}]}`, nil)
	tagged := filterItems(indexPaths(s))
	scalingCommit(t, s, nil, `{items: [{name: "a", v: 2}]}`, nil)
	untagged := filterItems(indexPaths(s))
	if !sameSet(tagged, untagged) {
		t.Errorf("the tag changed the path shape\n tagged:   %v\n untagged: %v", tagged, untagged)
	}
	for _, p := range untagged {
		if strings.Contains(p, "[") || strings.Contains(p, "items(") {
			t.Errorf("an element indexed by position or by a (key) segment: %q", p)
		}
	}
}

// TestKey_SchemaRouteIsAutoIDOnly: an auto-id declares an identity like a key does, and
// the generated id is the name. The recorded path is resolvable from the stored entry,
// which holds the element under that name.
func TestKey_SchemaRouteIsAutoIDOnly(t *testing.T) {
	s := openTestStorage(t)
	s.SetSchemaResolver(&api.StaticSchemaResolver{Schema: &api.Schema{
		AutoIDFields: []api.AutoIDField{{Path: "items", Field: "id"}},
	}})

	scalingCommit(t, s, nil, `{items: [{v: 1}]}`, nil)
	paths := filterItems(indexPaths(s))
	t.Logf("schema-keyed write, index paths: %v", paths)
	if !hasKeyedPath(paths, `items."(id=`) {
		t.Errorf("no element named by its generated id: %v", paths)
	}
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
