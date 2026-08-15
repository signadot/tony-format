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
