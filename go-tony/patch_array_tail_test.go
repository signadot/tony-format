package tony

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A positional array patch longer than the document appended its extra elements
// verbatim, uninterpreted, so an operation written past the end was stored as
// data:
//
//	o patch -y '{xs: [1, 2, !delete null]}' '{xs: [1, 2]}'
//	{xs: [1, 2, !delete null]}
//
// The same element meant two different things depending on where it fell --
// !delete at index 0 removed the element it met, !delete past the end became
// one -- and what it left behind was a delete marker sitting in the document as
// a value.
//
// !key was fixed for exactly this, and says so: an element the document does not
// have is being introduced, its patch is still a patch, and it is applied against
// an absent document. Absent is null. The positional branch now reads the same.
func TestArrayPatchPastTheEnd(t *testing.T) {
	tests := []struct {
		name      string
		doc, tony string
		want      string
	}{{
		// The report: a delete of an element that is not there resolves to
		// nothing, so nothing is what it adds.
		name: "a delete past the end",
		doc:  `{xs: [1, 2]}`,
		tony: `{xs: [1, 2, !delete null]}`,
		want: `{
  xs: [
    1
    2
  ]
}`,
	}, {
		name: "several of them",
		doc:  `{xs: [1]}`,
		tony: `{xs: [1, !delete null, !delete null]}`,
		want: `{
  xs: [
    1
  ]
}`,
	}, {
		// The other half of the same mistake: the op tag left on a value the
		// patch does introduce.
		name: "an insert past the end",
		doc:  `{xs: [1]}`,
		tony: `{xs: [1, !insert(t) 2]}`,
		want: `{
  xs: [
    1
    !t 2
  ]
}`,
	}, {
		name: "an op that computes what it adds",
		doc:  `{xs: [1]}`,
		tony: `{xs: [1, !nullify null]}`,
		want: `{
  xs: [
    1
    null
  ]
}`,
	}, {
		// Ordinary data past the end is what it always was: the element itself.
		name: "a plain value past the end",
		doc:  `{xs: [1]}`,
		tony: `{xs: [1, 2, {a: 3}]}`,
		want: `{
  xs: [
    1
    2
    {
      a: 3
    }
  ]
}`,
	}, {
		// In bounds it always worked, and this is what it has to keep meaning:
		// the two now agree.
		name: "a delete in bounds",
		doc:  `{xs: [1, 2]}`,
		tony: `{xs: [!delete null, 2]}`,
		want: `{
  xs: [
    2
  ]
}`,
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
			if got == nil {
				t.Fatal("the patch answered with no document")
			}
			if s := strings.TrimSpace(encode.MustString(got)); s != strings.TrimSpace(test.want) {
				t.Errorf("got\n%s\nwant\n%s", s, strings.TrimSpace(test.want))
			}
		})
	}
}
