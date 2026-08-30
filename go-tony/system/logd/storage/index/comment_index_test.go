package index

import (
	"slices"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// indexedPaths indexes src as a patch and answers the paths it was recorded at.
func indexedPaths(t *testing.T, src string) []string {
	t.Helper()
	n, err := parse.Parse([]byte(src), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	idx := NewIndex("")
	last := int64(0)
	e := &dlog.Entry{Commit: 1, LastCommit: &last}
	IndexPatch(idx, e, "A", 0, 1, 0, n, nil, nil)
	paths := []string{}
	for _, seg := range idx.AllSegments() {
		paths = append(paths, seg.KindedPath)
	}
	return paths
}

// TestCommentedPatchIndexesEveryPath: indexPatchRec switched on n.Type with cases
// for objects and arrays, and a head comment is a CommentType wrapping the value
// it precedes -- so the recursion stopped there and everything beneath went
// unindexed. A comment at the top of a patch left the root as the only recorded
// path, and a watch on a path inside the patch did not see the commit.
//
// That is data loss rather than comment loss, and it is the reason comments
// cannot be turned on before this is fixed (3cdjz00jh12krns4g1n0).
func TestCommentedPatchIndexesEveryPath(t *testing.T) {
	want := indexedPaths(t, "a:\n  b: 1\n")
	if !slices.Contains(want, "a.b") {
		t.Fatalf("the comment-free patch does not index a.b, so this test proves nothing: %v", want)
	}
	for _, tc := range []struct{ name, src string }{
		{"a comment above the document", "# note\na:\n  b: 1\n"},
		{"a comment inside the object", "a:\n  # note\n  b: 1\n"},
		{"a line comment on the leaf", "a:\n  b: 1 # note\n"},
		{"comments at both levels", "# top\na:\n  # inner\n  b: 1 # line\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := indexedPaths(t, tc.src)
			if !slices.Equal(got, want) {
				t.Errorf("indexed %v, and the same patch without comments indexes %v", got, want)
			}
		})
	}
}

// TestCommentedArrayPatchIndexesEveryElement: the same wrapper sits between an
// array and its elements, where the paths are positional and a truncated
// recursion loses every element at once.
func TestCommentedArrayPatchIndexesEveryElement(t *testing.T) {
	want := indexedPaths(t, "items:\n- a: 1\n- a: 2\n")
	if !slices.Contains(want, "items[1].a") {
		t.Fatalf("the comment-free patch does not index items[1].a: %v", want)
	}
	got := indexedPaths(t, "items:\n# about the list\n- a: 1\n- a: 2\n")
	if !slices.Equal(got, want) {
		t.Errorf("indexed %v, and the same patch without comments indexes %v", got, want)
	}
}
