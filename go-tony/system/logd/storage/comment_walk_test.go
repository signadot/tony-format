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

// TestKeyedAnnotationThroughComments: annotateKeyed tags the array a schema
// declares keyed, and patchHasUndeclaredKey looks for a !key the schema has not
// heard of. Both switched on the node's type, so a comment above the array hid
// it from each -- the first leaving a materialized state unkeyed, the second
// reporting a scope safe when it is not.
func TestKeyedAnnotationThroughComments(t *testing.T) {
	keys := map[string]string{"users": "id"}
	for _, src := range []string{
		"users:\n- id: a\n",
		"# note\nusers:\n- id: a\n",
		"users:\n# note\n- id: a\n",
	} {
		n := parseCommented(t, src)
		annotateKeyed(n, "", keys)
		users, err := n.GetKPath("users")
		if err != nil {
			t.Fatal(err)
		}
		if field, keyed := users.KeyField(); !keyed || field != "id" {
			t.Errorf("%q: annotateKeyed left the array keyed by %q (keyed=%v)", src, field, keyed)
		}
	}

	// The undeclared-key check, with nothing declared: the !key the patch carries
	// is the one the schema has never heard of.
	for _, src := range []string{
		"users: !key(id)\n- id: a\n",
		"# note\nusers: !key(id)\n- id: a\n",
		"a:\n  # note\n  users: !key(id)\n  - id: a\n",
	} {
		if !patchHasUndeclaredKey(parseCommented(t, src), "", map[string]string{}) {
			t.Errorf("%q: the undeclared !key was not seen", src)
		}
	}
}
