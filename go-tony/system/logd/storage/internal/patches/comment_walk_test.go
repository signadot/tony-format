package patches

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

func walkedPaths(t *testing.T, src string) []string {
	t.Helper()
	paths := []string{}
	walkIRTree(parseCommented(t, src), "", func(_ *ir.Node, path string) {
		paths = append(paths, path)
	})
	slices.Sort(paths)
	return paths
}

// TestWalkIRTreeThroughComments: the patch index is built by walking a patch and
// recording the path of each node. The walk switched on node.Type, and a head
// comment is a wrapper, so everything beneath a comment went unrecorded and a
// lookup at those paths found no patches (3cdjz00jh12krns4g1n0).
func TestWalkIRTreeThroughComments(t *testing.T) {
	want := walkedPaths(t, "a:\n  b: 1\n")
	if !slices.Contains(want, "a.b") {
		t.Fatalf("the comment-free patch does not walk to a.b: %v", want)
	}
	for _, src := range []string{"# note\na:\n  b: 1\n", "a:\n  # note\n  b: 1\n"} {
		got := walkedPaths(t, src)
		if !slices.Equal(got, want) {
			t.Errorf("%q walks %v, and the same patch without comments walks %v", src, got, want)
		}
	}
}

// TestCommentedNodeIsARoot: a patch root is read from the entry's shape, and a comment
// is part of what an entry says. A commented node is collected whole, wrapper included,
// at the path the comment is on -- descending past the wrapper to the value beneath
// would apply the value and lose the comment, so a replay would disagree with the head
// over a comment while every value matched. A comment above the first field of a block
// is the block's, so `# note` inside `a:` roots the patch at a.
func TestCommentedNodeIsARoot(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want string
	}{
		{"a:\n  b: 1\n", "a.b"},
		{"a:\n  # note\n  b: 1\n", "a"},
		{"# note\na:\n  b: 1\n", ""},
	} {
		n := parseCommented(t, tc.src)
		var got []string
		walkAndCollectPatchRoots(n, "", func(node *ir.Node, path string) {
			got = append(got, path)
			if v, err := ir.Uncomment(node).GetKPath("a.b"); path == "" && (err != nil || v == nil) {
				t.Errorf("%q: the collected root does not carry a.b: %v", tc.src, err)
			}
		})
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("%q: roots at %q, want one at %q", tc.src, got, tc.want)
		}
	}
}
