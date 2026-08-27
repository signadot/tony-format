package encode_test

import (
	"bytes"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A head comment on an array's FIRST element was written on the line above its
// "- ", where it read back as a comment on the ARRAY:
//
//	Array[ Comment("# c"){5} ]   ->   Comment("# c"){ Array[5] }
//
// and merged with whatever the array already had, so two comments about two things
// became one about the outer one. One `o v -c` pass did it and the result was a
// fixed point, so nothing said so; `o v -w` writes it into the file.
//
// Only the first is affected. Above a LATER marker that line belongs to its element
// and nothing else, so the comment goes there -- the spelling the docs use, and the
// one already in every document (haw04psch12ksnn2j1n0).
func TestElementHeadComment(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{{
		name: "the first element's comment stays on the element",
		in:   "- # c\n  5\n",
		want: "- # c\n  5\n",
	}, {
		name: "a later element's stays above its marker",
		in:   "- 1\n# c\n- 2\n",
		want: "- 1\n# c\n- 2\n",
	}, {
		name: "the array's own comment and the first element's are kept apart",
		in:   "# about the array\n- # about the element\n  1\n",
		want: "# about the array\n- # about the element\n  1\n",
	}, {
		name: "an object element",
		in:   "- # c\n  x: 1\n  y: 2\n",
		want: "- # c\n  x: 1\n  y: 2\n",
	}, {
		// The '|' opens a line of its own here, so its content is a level in
		// from THAT line: 4, not the 2 it takes when it shares the '- ' line.
		name: "a block literal element, whose indent follows the '|' down",
		in:   "- # c\n  | # why\n    ape\n    ape\n",
		want: "- # c\n  | # why\n    ape\n    ape\n",
	}, {
		name: "under a field",
		in:   "a:\n- # c\n  1\n",
		want: "a:\n- # c\n  1\n",
	}, {
		name: "the docs' spelling is left alone",
		in:   "# about rule a\n- name: a\n# about rule b\n- name: b\n",
		want: "# about rule a\n- name: a\n# about rule b\n- name: b\n",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc, err := parse.Parse([]byte(test.in), parse.ParseComments(true))
			if err != nil {
				t.Fatalf("parse: %s", err)
			}
			got := enc(t, doc)
			if got != test.want {
				t.Errorf("wrote\n%q\nwant\n%q", got, test.want)
			}
			// What it says about is what matters, and only the IR records that.
			again, err := parse.Parse([]byte(got), parse.ParseComments(true))
			if err != nil {
				t.Fatalf("reparse %q: %s", got, err)
			}
			if !doc.DeepEqualWithComments(again) {
				t.Errorf("the comment moved: %q came back as %q with a different tree",
					test.in, got)
			}
			if twice := enc(t, again); twice != got {
				t.Errorf("a second pass wrote\n%q\nafter\n%q", twice, got)
			}
		})
	}
}

func enc(t *testing.T, doc *ir.Node) string {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := encode.Encode(doc, buf, encode.EncodeComments(true)); err != nil {
		t.Fatalf("encode: %s", err)
	}
	return buf.String()
}
