package tony_test

import (
	"strings"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
)

func pc(t *testing.T, src string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(src), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return n
}

func shownWithComments(t *testing.T, n *ir.Node) string {
	t.Helper()
	if n == nil {
		return ""
	}
	var b strings.Builder
	if err := encode.Encode(n, &b, encode.EncodeComments(true)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b.String()
}

// TestDiffComments: a comment-only change had no delta at all -- Diff answered
// nil, so nothing could record that a comment had changed, and DiffComments
// existed with no function that would take it (Diff is passed around as a
// libdiff.DiffFunc and cannot grow options). DiffWith takes them.
func TestDiffComments(t *testing.T) {
	for _, tc := range []struct{ name, a, b string }{
		{"a head comment changed", "# old\nname: svc\n", "# new\nname: svc\n"},
		{"a head comment added", "name: svc\n", "# new\nname: svc\n"},
		{"a head comment removed", "# old\nname: svc\n", "name: svc\n"},
		{"a line comment changed", "name: svc # old\n", "name: svc # new\n"},
		{"a comment on a nested value", "a:\n  # old\n  b: 1\n", "a:\n  # new\n  b: 1\n"},
		{"a comment and a value together", "# old\nname: was\n", "# new\nname: now\n"},
		{"both positions at once", "# oldh\nname: svc # oldl\n", "# newh\nname: svc # newl\n"},
		{"head changed, line untouched", "# oldh\nname: svc # keep\n", "# newh\nname: svc # keep\n"},
		{"line removed, head untouched", "# keep\nname: svc # gone\n", "# keep\nname: svc\n"},

		// An array element's comment wraps the element, and the by-index diff
		// summarizes each element to align the two arrays. The summary had no
		// case for a comment, so it panicked ("type") rather than diffing --
		// `o diff -c` died on any array holding a commented element, whether or
		// not the comment was the thing that changed.
		{"a comment on an array element", "items:\n- # old\n  a\n- b\n", "items:\n- # new\n  a\n- b\n"},
		{"a comment added to an array element", "items:\n- a\n- b\n", "items:\n- # new\n  a\n- b\n"},
		{"a comment removed from an array element", "items:\n- # old\n  a\n- b\n", "items:\n- a\n- b\n"},
		{"a comment on the last element", "items:\n- a\n- # old\n  b\n", "items:\n- a\n- # new\n  b\n"},
		{"a line comment on an array element", "items:\n- a # old\n- b\n", "items:\n- a # new\n- b\n"},
		{"a comment on an element of an array of objects", "items:\n- # old\n  name: a\n- name: b\n", "items:\n- # new\n  name: a\n- name: b\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := pc(t, tc.a), pc(t, tc.b)

			// Blind by default: a comment change is not a data change.
			if d := tony.Diff(a, b); d != nil && tc.name != "a comment and a value together" {
				t.Errorf("the plain diff reported a comment-only change: %s", shownWithComments(t, d))
			}

			d := tony.DiffWith(a, b, tony.DiffComments(true))
			if d == nil {
				t.Fatalf("DiffComments produced no delta for %q -> %q", tc.a, tc.b)
			}

			got, err := tony.Patch(a, d, mergeop.Comments(true))
			if err != nil {
				t.Fatalf("applying the delta: %v (delta %s)", err, shownWithComments(t, d))
			}
			if shownWithComments(t, got) != shownWithComments(t, b) {
				t.Errorf("Patch(a, DiffWith(a,b)) did not arrive at b:\n got %q\nwant %q\ndelta %s",
					shownWithComments(t, got), shownWithComments(t, b), shownWithComments(t, d))
			}
		})
	}
}

// TestDiffCommentsLeavesDataDiffsAlone: with the option on, a change with no
// comments in it produces what it always did.
func TestDiffCommentsLeavesDataDiffsAlone(t *testing.T) {
	for _, tc := range []struct{ a, b string }{
		{"a: 1\n", "a: 2\n"},
		{"a: 1\n", "a: 1\nb: 2\n"},
		{"items:\n- a\n- b\n", "items:\n- a\n"},
		// A data change to an array which holds a commented element: the
		// alignment summarizes every element, so this reached the same panic
		// even though no comment changed.
		{"items:\n- # note\n  a\n- b\n", "items:\n- # note\n  a\n"},
		{"items:\n- # note\n  a\n", "items:\n- # note\n  a\n- c\n"},
		// An UNCHANGED comment on the node whose value changed. The wrappers
		// used to come off one side at a time, so the pair met the comment
		// comparison again half-unwrapped and the comment read as new: here that
		// meant a !replace carrying the whole subtree, for a change to one leaf.
		{"a:\n  # note\n  b: 1\n", "a:\n  # note\n  b: 2\n"},
		{"# note\nname: was\n", "# note\nname: now\n"},
		{"a:\n  # note\n  b:\n    c: 1\n", "a:\n  # note\n  b:\n    c: 2\n"},
		{"name: svc # note\nn: 1\n", "name: svc # note\nn: 2\n"},
	} {
		a, b := pc(t, tc.a), pc(t, tc.b)
		plain := tony.Diff(a, b)
		withC := tony.DiffWith(a, b, tony.DiffComments(true))
		if encode.MustString(plain) != encode.MustString(withC) {
			t.Errorf("%q -> %q: the option changed a data-only diff:\n%s\n%s",
				tc.a, tc.b, encode.MustString(plain), encode.MustString(withC))
		}
	}
}
