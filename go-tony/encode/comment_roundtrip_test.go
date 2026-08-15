package encode_test

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

// TestCommentRoundTrip: a document written with comments comes back as it was
// written. Two positions did not.
//
//   - "a: # c" over an indented block: the comment parses onto the container,
//     and a block container has no line of its own to end, so nothing wrote it
//     and it was LOST. A bracketed one writes its own after the closing token.
//   - "# above c" before a field whose value is a scalar: the comment parses
//     onto that value, and writing it where the value goes pushed the scalar
//     onto a line of its own -- the document changed shape.
func TestCommentRoundTrip(t *testing.T) {
	// A non-empty bracket collection is spread over lines whatever it was
	// written as, so its want is not its src; what matters is where the comment
	// lands.
	for _, tc := range []struct{ src, want string }{
		{"# above\nb: 1\n", ""},
		{"b: 1 # latch\n", ""},
		{"a:\n  # above b\n  b: 1\n", ""},
		{"a: # latch on a\n  b: 1\n", ""},
		{"a: # latch\n- x\n", ""},
		{"a:\n  b: 1 # latch on b\n", ""},
		{"a:\n- x\n- y # latch on y\n", ""},
		{"a:\n  # above b\n  b: 1\n  # above c\n  c: 2\n", ""},
		{"a: {b: 1} # c\n", "a: {\n  b: 1\n} # c\n"},
		{"# doc\na:\n  # about b\n  b: 1\n  c:\n  # about first\n  - x\n  - y # latch\nz: 2\n", ""},
	} {
		want := tc.want
		if want == "" {
			want = tc.src
		}
		t.Run(strings.ReplaceAll(tc.src, "\n", "\\n"), func(t *testing.T) {
			n, err := parse.Parse([]byte(tc.src), parse.ParseComments(true))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			var b strings.Builder
			if err := encode.Encode(n, &b, encode.EncodeComments(true)); err != nil {
				t.Fatalf("encode: %v", err)
			}
			if b.String() != want {
				t.Errorf("round trip changed the document:\n in %q\nout %q\nwant %q", tc.src, b.String(), want)
			}
		})
	}
}
