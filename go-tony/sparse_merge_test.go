package tony

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// sparseKeysAreIntegers says whether every key of a sparse array is an integer key, as
// ir.FromIntKeysMapAt builds one: a Number holding the key, its decimal in String. A
// comparison by field name cannot see the difference -- "0" and 0 are both named "0".
func sparseKeysAreIntegers(n *ir.Node) error {
	if n == nil || !isSparse(n) {
		return nil
	}
	for i, f := range n.Fields {
		if f.Type != ir.NumberType || f.Int64 == nil || f.String != strconv.FormatInt(*f.Int64, 10) {
			return fmt.Errorf("key %d of the sparse array is %s %q, not an integer key", i, f.Type, f.String)
		}
	}
	return nil
}

// A merge into a sparse array answers a sparse array: integer keys, one !sparsearray. It
// answered a plain object's string keys under the sparse array's tag, and the tag twice,
// !sparsearray.sparsearray (07g0rn9xh12ksz5xmdn0). A patch of the other kind replaces,
// as an array patch does over a non-array; an untagged empty object merges into either.
func TestAMergeKeepsASparseArraySparse(t *testing.T) {
	p := func(s string) *ir.Node {
		n, err := parse.Parse([]byte(s))
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return n
	}
	for _, test := range []struct{ doc, patch, want string }{
		{`{x: !sparsearray {0: a, 1: b}}`, `{x: !sparsearray {1: c}}`, `{x: !sparsearray {0: a, 1: c}}`},
		{`{x: !sparsearray {0: a, 1: b}}`, `{x: !sparsearray {2: c}}`, `{x: !sparsearray {0: a, 1: b, 2: c}}`},
		{`{}`, `{x: !sparsearray {0: y}}`, `{x: !sparsearray {0: y}}`},
		{`{}`, `{x: !insert(sparsearray) {0: y}}`, `{x: !sparsearray {0: y}}`},
		{`{x: !sparsearray {0: a, 1: b}}`, `{x: !sparsearray {0: !delete a}}`, `{x: !sparsearray {1: b}}`},
		{`{x: {a: 1}}`, `{x: !sparsearray {0: y}}`, `{x: !sparsearray {0: y}}`},
		{`{x: !sparsearray {0: y}}`, `{x: {a: 1}}`, `{x: {a: 1}}`},
		{`{x: !sparsearray {0: y}}`, `{x: {}}`, `{x: !sparsearray {0: y}}`},
	} {
		t.Run(test.doc+" <- "+test.patch, func(t *testing.T) {
			got, err := Patch(p(test.doc), p(test.patch))
			if err != nil {
				t.Fatalf("patch: %v", err)
			}
			want := p(test.want)
			if left := leftover(got, want); left != nil {
				t.Errorf("got %s, want %s", encode.MustString(got), encode.MustString(want))
			}
			x, _ := got.GetKPath("x")
			if err := sparseKeysAreIntegers(x); err != nil {
				t.Error(err)
			}
			if x != nil && strings.Count(x.Tag, "sparsearray") > 1 {
				t.Errorf("x is tagged %q", x.Tag)
			}
		})
	}
}

func isSparse(n *ir.Node) bool { return n != nil && ir.TagHas(n.Tag, ir.IntKeysTag) }
