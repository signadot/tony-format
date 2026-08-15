package patches

import (
	"slices"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
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

// TestPatchRootsThroughComments: a patch root is found by its tag, which sits on
// the node INSIDE a comment wrapper. Left wrapped, the root was neither collected
// nor descended into, so the streaming processor applied nothing at all -- the
// write was accepted and did not happen.
func TestPatchRootsThroughComments(t *testing.T) {
	for _, src := range []string{"a:\n  b: 1\n", "# note\na:\n  b: 1\n"} {
		n := parseCommented(t, src)
		root := ir.Uncomment(n)
		root.Tag = ir.TagCompose(tx.PatchRootTag, nil, root.Tag)

		found := 0
		walkAndCollectPatchRoots(n, "", func(node *ir.Node, path string) {
			found++
			if path != "" {
				t.Errorf("%q: patch root found at %q, and it is the document root", src, path)
			}
			if v, err := node.GetKPath("a.b"); err != nil || v == nil {
				t.Errorf("%q: the collected root does not carry a.b: %v", src, err)
			}
		})
		if found != 1 {
			t.Errorf("%q: collected %d patch roots, want 1", src, found)
		}
	}
}
