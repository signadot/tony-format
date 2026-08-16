package parse_test

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A document read and written comes back the same, and then stays the same.
//
// A sparse array did not. FromIntKeysMapAt sets !sparsearray only when the node
// does not already carry it, and the parser composed the WRITTEN tag on top
// without that check -- so a document written with the tag it renders with grew a
// label on every pass:
//
//	v: {3: a}  ->  !sparsearray  ->  !sparsearray.sparsearray  ->  ...
//
// Three passes, because two would have caught only the first doubling and this
// grew without bound.
func TestSparseArrayTagDoesNotGrow(t *testing.T) {
	for _, src := range []string{
		"v: {3: a}\n",
		"v: !sparsearray {3: a}\n",
		"v: !mine {3: a}\n",
		"v: !sparsearray {3: a, 7: b}\n",
	} {
		t.Run(strings.TrimSpace(src), func(t *testing.T) {
			once := renderOnce(t, src)
			twice := renderOnce(t, once)
			thrice := renderOnce(t, twice)
			if once != twice || twice != thrice {
				t.Errorf("a round trip changed the document:\n 1: %q\n 2: %q\n 3: %q", once, twice, thrice)
			}
			if n := strings.Count(once, "sparsearray"); n != 1 {
				t.Errorf("%q names sparsearray %d times, want 1", once, n)
			}
		})
	}
}

func renderOnce(t *testing.T, src string) string {
	t.Helper()
	node, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	var b strings.Builder
	if err := encode.Encode(node, &b); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b.String()
}
