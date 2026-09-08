package storage

import (
	"slices"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func parseCommented(t *testing.T, src string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(src), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return n
}

// TestTopLevelKPathsThroughComments: extractTopLevelKPaths names the paths a
// commit touches, and it switched on patch.Type -- so a comment above the
// document made a patch look like it touched nothing (3cdjz00jh12krns4g1n0).
func TestTopLevelKPathsThroughComments(t *testing.T) {
	want := extractTopLevelKPaths(parseCommented(t, "a: 1\nb: 2\n"))
	if len(want) != 2 {
		t.Fatalf("the comment-free patch names %v", want)
	}
	for _, src := range []string{"# note\na: 1\nb: 2\n", "# one\n# two\na: 1\nb: 2\n"} {
		got := extractTopLevelKPaths(parseCommented(t, src))
		if !slices.Equal(got, want) {
			t.Errorf("%q names %v, and the same patch without comments names %v", src, got, want)
		}
	}
}
