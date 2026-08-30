package tony

import (
	"bytes"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

const (
	trimDoc  = "verse:\n  meta:\n    rev: 7\n    status: ready\n    notes: keep-out\n  big:\n  - 1\n  - 2\n"
	trimList = "- {name: a, x: 1}\n- {name: b, x: 2}\n"
)

// A tagged pattern used to be a test and nothing more: Trim matched it and returned the
// document WHOLE, so a projection under a path could not be written -- `!at(p) {f: !pass
// null}`, the composition the vocabulary invites, answered everything and looked like it
// had worked. An operator that names WHERE now carries the trim with it: !at walks, !all
// distributes (599k23e4h12krhw3gdn0).
//
// What Trim does NOT do is decide. It shapes; a scalar the pattern disagrees with comes
// back regardless, exactly as it always has, and whether the document qualifies at all is
// Match's question -- FilterState is where the two are put together. So the only exclusion
// here is a path which names nothing, where there is nothing to project from.
func TestTrimLocatingOps(t *testing.T) {
	// want == "" means the pattern excludes the document, which Trim reports as nil.
	cases := []struct {
		name, doc, match, want string
	}{
		{"at names the fields under a path", trimDoc,
			"!at(verse.meta) {rev: !pass null}",
			"verse:\n  meta:\n    rev: 7\n"},
		{"at names two fields", trimDoc,
			"!at(verse.meta) {rev: !pass null, status: !pass null}",
			"verse:\n  meta:\n    rev: 7\n    status: ready\n"},
		{"at reaches a leaf", trimDoc,
			"!at(verse.meta.rev).pass null",
			"verse:\n  meta:\n    rev: 7\n"},
		{"a path naming nothing excludes", trimDoc,
			"!at(nope.here).pass null", ""},
		{"a wild path keeps every node it reaches", trimDoc,
			"!at(verse.*).pass null",
			"verse:\n  big:\n  - 1\n  - 2\n  meta:\n    rev: 7\n    status: ready\n    notes: keep-out\n"},
		{"a predicate under the walk shapes nothing, but the walk still does", trimDoc,
			"!at(verse.meta).not {rev: 9}",
			"verse:\n  meta:\n    rev: 7\n    status: ready\n    notes: keep-out\n"},
		{"a predicate over the walk shapes nothing at all", trimDoc,
			"!not.at(verse.meta) {rev: 9}", trimDoc},
		{"an element keeps its place, and the array closes the gap", "items:\n" + trimList,
			"!at(items[1]) {name: !pass null}",
			"items:\n- {\n  name: b\n}\n"},
		{"all distributes over the elements", trimList,
			"!all {name: !pass null}",
			"- {\n  name: a\n}\n- {\n  name: b\n}\n"},
		{"all distributes over an object's values", "a: {k: 1, j: 2}\nb: {k: 3, j: 4}\n",
			"!all {k: !pass null}",
			"a: {\n  k: 1\n}\nb: {\n  k: 3\n}\n"},
		{"all excludes when one element fails a test", "- ab\n- cd\n",
			"!all.glob \"a*\"", ""},
		{"an untagged pattern is unchanged", trimDoc,
			"{verse: {meta: {rev: !pass null}}}",
			"verse:\n  meta:\n    rev: 7\n"},
	}

	for _, c := range cases {
		d, err := parse.Parse([]byte(c.doc))
		if err != nil {
			t.Errorf("%s: doc: %v", c.name, err)
			continue
		}
		m, err := parse.Parse([]byte(c.match))
		if err != nil {
			t.Errorf("%s: match: %v", c.name, err)
			continue
		}
		got := Trim(m, d)
		if c.want == "" {
			if got != nil {
				t.Errorf("%s: expected exclusion, got %q", c.name, encTrim(t, got))
			}
			continue
		}
		if got == nil {
			t.Errorf("%s: excluded, want %q", c.name, c.want)
			continue
		}
		if g := encTrim(t, got); g != c.want {
			t.Errorf("%s:\n  got  %q\n  want %q", c.name, g, c.want)
		}
	}
}

func encTrim(t *testing.T, n *ir.Node) string {
	t.Helper()
	var buf bytes.Buffer
	if err := encode.Encode(n, &buf); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.String()
}
