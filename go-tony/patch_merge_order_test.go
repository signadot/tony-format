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

// fieldOrder is the sequence of top-level keys as written, merge keys included.
// objPatchYWith records where a merge sits by taking the ADDRESS of the
// preceding value's ParentField, so anything that changes which node is a
// value -- wrapping one in a comment, say -- can move a merge relative to the
// keys it was written between.
func fieldOrder(t *testing.T, n *ir.Node) []string {
	t.Helper()
	var b strings.Builder
	if err := encode.Encode(n, &b, encode.EncodeComments(true)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var out []string
	for _, line := range strings.Split(b.String(), "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "-") || strings.TrimSpace(line) == "" {
			continue // nested content, not a top-level key
		}
		if strings.HasPrefix(line, "#") {
			continue // a head comment line, not a key
		}
		if i := strings.Index(line, ":"); i > 0 {
			out = append(out, strings.Trim(line[:i], `"`))
		}
	}
	return out
}

func parseDoc(t *testing.T, src string, comments bool) *ir.Node {
	t.Helper()
	opts := []parse.ParseOption{}
	if comments {
		opts = append(opts, parse.ParseComments(true))
	}
	n, err := parse.Parse([]byte(src), opts...)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return n
}

// TestPatchKeepsMergeKeyOrder: a merge key belongs where it was written -- what
// it merges over depends on it -- and comments must not move it, whether they
// are dropped or kept.
func TestPatchKeepsMergeKeyOrder(t *testing.T) {
	const plain = "a: 1\n<<: {m: 1}\nz: 2\n"
	const commented = "# lead\na: 1 # latch\n<<: {m: 1}\n# above z\nz: 2\n"

	want := fieldOrder(t, mustPatch(t, parseDoc(t, plain, false)))
	if len(want) < 3 {
		t.Fatalf("the plain document did not survive patching: %v", want)
	}

	for _, tc := range []struct {
		name string
		src  string
		opts []mergeop.PatchOpt
	}{
		{"plain, comments parsed", plain, nil},
		{"commented, comments dropped", commented, nil},
		{"commented, comments kept", commented, []mergeop.PatchOpt{mergeop.Comments(true)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := fieldOrder(t, mustPatch(t, parseDoc(t, tc.src, true), tc.opts...))
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("key order is %v, want %v", got, want)
			}
		})
	}
}

func mustPatch(t *testing.T, patch *ir.Node, opts ...mergeop.PatchOpt) *ir.Node {
	t.Helper()
	got, err := tony.Patch(ir.Null(), patch, opts...)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	return got
}
