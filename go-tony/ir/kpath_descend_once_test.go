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

// A depth bounds every descent (WithDepth): `a..` at depth 1 is a and its children,
// of every kind, and `..c` at depth 2 reaches a c at most two levels down. Depth 0 is
// a descent that takes nothing. It is what logd answers for the same path and depth.
func TestDescendDepth(t *testing.T) {
	doc := obj(
		"a", obj(
			"b", obj("b", obj("c", FromInt(1)), "c", FromInt(2)),
			"x", arr(obj("c", FromInt(3))),
			"c", FromInt(4),
		),
		"c", FromInt(5),
	)
	for _, tc := range []struct {
		pattern string
		depth   int
		want    []string
	}{
		{"a..", 0, []string{"a"}},
		{"a..", 1, []string{"a", "a.b", "a.x", "a.c"}},
		{"a..", 2, []string{"a", "a.b", "a.b.b", "a.b.c", "a.x", "a.x[0]", "a.c"}},
		{"..c", 0, []string{"c"}},
		{"..c", 1, []string{"a.c", "c"}},
		// Two segments down at most: a.x[0].c is three.
		{"..c", 2, []string{"a.b.c", "a.c", "c"}},
		{"a..b..c", 1, []string{"a.b.b.c", "a.b.c"}},
		{"..", 1, []string{"", "a", "c"}},
	} {
		found, err := doc.ListKPathWith(nil, tc.pattern, WithDepth(tc.depth))
		if err != nil {
			t.Fatalf("%s: %v", tc.pattern, err)
		}
		var got []string
		for _, n := range found {
			got = append(got, n.KPath())
		}
		if strings.Join(got, " ") != strings.Join(tc.want, " ") {
			t.Errorf("%q at depth %d answered %v, want %v", tc.pattern, tc.depth, got, tc.want)
		}
	}
}

// The depth rule is kpath's, and ir's entry applies it: a depth on a path with no
// `..` is an error, and so is a negative one.
func TestDescendDepthIsChecked(t *testing.T) {
	doc := obj("a", obj("b", FromInt(1)))
	for _, tc := range []struct {
		pattern string
		depth   int
	}{{"a.b", 1}, {"a.*", 0}, {"a..", -2}} {
		if _, err := doc.ListKPathWith(nil, tc.pattern, WithDepth(tc.depth)); err == nil {
			t.Errorf("%q at depth %d was not refused", tc.pattern, tc.depth)
		}
	}
}
