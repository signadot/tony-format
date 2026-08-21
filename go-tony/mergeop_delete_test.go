package tony

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

// Seven places a composing operation dereferenced what a delete beneath it gave
// back. No node is how this library says the value is gone -- it is what
// `!delete` answers with, and what every op composed over one has to expect --
// and each of these wrote to it instead: ir.FromSlice on the element, WithTag on
// the value, SplitChild on the branch that was never written.
//
// Each was a distinct panic, verified one at a time against the unfixed code:
// arraydiff.go:191 by way of ir/node.go:261, insert.go:54, addtag.go:45,
// retag.go:48 and rmtag.go:47 by way of ir/node.go:32, and if.go:71 and if.go:73
// by way of mergeop/split_child.go:8.
//
// They are the same mistake `!dive` had (see dive_delete_test.go), one frame out
// in seven directions.
func TestDeleteComposedUnderAnOp(t *testing.T) {
	tests := []struct {
		name      string
		doc, tony string
		// want is the document that results, or "" for no document at all.
		want string
	}{{
		// !arraydiff's own delete is positional and was handled. This is a
		// NESTED op which deletes -- a dive, an if, an all over a scalar --
		// reaching the branch that applies an arbitrary patch to an element.
		name: "an element an arraydiff dived into",
		doc:  `{xs: [{a: 1}, {b: 2}]}`,
		tony: `{xs: !arraydiff {0: !dive [{match: {a: 1}, patch: !delete null}]}}`,
		want: `{
  xs: [
    {
      b: 2
    }
  ]
}`,
	}, {
		name: "an element an arraydiff's if deleted",
		doc:  `{xs: [1, 2]}`,
		tony: `{xs: !arraydiff {0: !if {if: 1, then: !delete null, else: !pass null}}}`,
		want: `{
  xs: [
    2
  ]
}`,
	}, {
		// The four tag ops. Writing a tag onto a value that is gone is not a
		// thing to do, so the delete carries: absence is the result, tag and
		// all.
		name: "a delete under an insert that tags",
		doc:  `{a: 1}`,
		tony: `{a: !insert(x).delete null}`,
		want: `{}`,
	}, {
		name: "a delete under an addtag",
		doc:  `{a: 1}`,
		tony: `{a: !addtag(x).delete null}`,
		want: `{}`,
	}, {
		name: "a delete under a rmtag",
		doc:  `{a: !x 1}`,
		tony: `{a: !rmtag(x).delete null}`,
		want: `{}`,
	}, {
		name: "a delete under a retag",
		doc:  `{a: !p 1}`,
		tony: `{a: !retag(p,q).delete null}`,
		want: `{}`,
	}, {
		// An if admits then or else or both, so a one-sided one is a legitimate
		// patch -- and the delete is exactly what it is usually written for.
		name: "an if with only an else, matching",
		doc:  `{a: 1, b: 2}`,
		tony: `{a: !if {if: 1, else: !delete null}}`,
		want: `{
  a: 1
  b: 2
}`,
	}, {
		name: "an if with only an else, missing",
		doc:  `{a: 1, b: 2}`,
		tony: `{a: !if {if: 3, else: !delete null}}`,
		want: `{
  b: 2
}`,
	}, {
		name: "an if with only a then, matching",
		doc:  `{a: 1, b: 2}`,
		tony: `{a: !if {if: 1, then: !delete null}}`,
		want: `{
  b: 2
}`,
	}, {
		name: "an if with only a then, missing",
		doc:  `{a: 1, b: 2}`,
		tony: `{a: !if {if: 3, then: !delete null}}`,
		want: `{
  a: 1
  b: 2
}`,
	}, {
		// The shape all.go's own comment names, now that the else it needs can
		// stand on its own.
		name: "every field that does not match, deleted",
		doc:  `{xs: {a: 1, b: 2, c: 1}}`,
		tony: `{xs: !all.if {if: 1, else: !delete null}}`,
		want: `{
  xs: {
    a: 1
    c: 1
  }
}`,
	}, {
		// The whole document, from under an op that would have tagged it.
		name: "the root deleted under an addtag",
		doc:  `{a: 1}`,
		tony: `!addtag(x).delete null`,
		want: ``,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc, err := parse.Parse([]byte(test.doc))
			if err != nil {
				t.Fatalf("doc: %s", err)
			}
			patch, err := parse.Parse([]byte(test.tony))
			if err != nil {
				t.Fatalf("patch: %s", err)
			}
			got, err := Patch(doc, patch)
			if err != nil {
				t.Fatalf("patch: %s", err)
			}
			if test.want == "" {
				if got != nil {
					t.Errorf("the document was deleted but came back as %s",
						encode.MustString(got))
				}
				return
			}
			if got == nil {
				t.Fatal("the patch answered with no document")
			}
			if s := strings.TrimSpace(encode.MustString(got)); s != strings.TrimSpace(test.want) {
				t.Errorf("got\n%s\nwant\n%s", s, strings.TrimSpace(test.want))
			}
		})
	}
}
