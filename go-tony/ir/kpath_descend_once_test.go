package ir

import (
	"strings"
	"testing"
)

// A descent answers each node ONCE, in document order. `..` used to offer every
// node beneath to the rest of the pattern, which found a node once per way of
// reaching it -- a..b..c answered one c twice, `....c` three times -- and grouped
// the answers by the node offered rather than by where the document has them. A
// set is a set, and the order is the document's, which is the order logd's set read
// answers the same query in over the store (th7sdhvyh12ksjtfn9n0).
func TestDescendAnswersEachNodeOnceInDocumentOrder(t *testing.T) {
	doc := obj(
		"a", obj(
			"b", obj(
				"b", obj("c", FromInt(1)),
				"c", FromInt(2),
			),
			"x", arr(obj("c", FromInt(3))),
			"c", FromInt(4),
		),
		"c", FromInt(5),
	)
	for _, tc := range []struct {
		pattern string
		want    []string
	}{
		// Two descents, two derivations of a.b.b.c: once.
		{"a..b..c", []string{"a.b.b.c", "a.b.c"}},
		// Two descents in a row are one.
		{"a....c", []string{"a.b.b.c", "a.b.c", "a.x[0].c", "a.c"}},
		{"a..c", []string{"a.b.b.c", "a.b.c", "a.x[0].c", "a.c"}},
		// Document order: the deeper c under a comes before the c beside a, where
		// the offered-node walk answered the shallow c first.
		{"..c", []string{"a.b.b.c", "a.b.c", "a.x[0].c", "a.c", "c"}},
		// Pre-order: a node before what is under it, the root first of all.
		{"a..", []string{"a", "a.b", "a.b.b", "a.b.b.c", "a.b.c", "a.x", "a.x[0]", "a.x[0].c", "a.c"}},
		{"a..b.c", []string{"a.b.b.c", "a.b.c"}},
		{"a..[*]", []string{"a.x[0]"}},
		{"a..b.*", []string{"a.b.b", "a.b.b.c", "a.b.c"}},
		{"..nope", nil},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			found, err := doc.ListKPath(nil, tc.pattern)
			if err != nil {
				t.Fatalf("ListKPath: %v", err)
			}
			var got []string
			for _, n := range found {
				got = append(got, n.KPath())
			}
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Errorf("%q answered %v, want %v", tc.pattern, got, tc.want)
			}
		})
	}
	// The root itself, with `..` alone: the first answer, and answered once.
	found, err := doc.ListKPath(nil, "..")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 11 || found[0].KPath() != "" {
		t.Errorf("`..` answered %d nodes, first at %q; want 11, the root first", len(found), found[0].KPath())
	}
}
