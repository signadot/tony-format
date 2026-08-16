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

// A head comment on a key whose value is TAGGED. The tag is written next to the
// key, so a comment after the colon separated the key from its own tag and the
// tag, the comment and the value all landed at column 0 --
//
//	b:
//	# note
//	!and
//	[ x  y ]
//
// which no longer says which key the value belongs to. It did not re-parse, and
// had it happened to, the meaning would have changed silently
// (jjthyd92h12ks8c1g5n0). A bracketed collection is spread over lines whatever
// it was written as, which is why the flow cases have a want of their own.
func TestCommentRoundTripTaggedValue(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"a: 1\n# note\nb: !and [x, y]\n", "a: 1\n# note\nb: !and\n[\n  x\n  y\n]\n"},
		{"a: 1\n# note\nb: !and\n- x\n- y\n", ""},
		{"a: 1\n# note\nb: !obj\n  k: v\n", ""},
		{"# note\nb: !glob \"x*\"\n", "# note\nb: !glob x*\n"}, // the quotes are not needed and not kept
		{"outer:\n  # note\n  b: !and\n  - x\n", ""},
		{"# one\n# two\nb: !and\n- x\n", ""},
		{"a: 1\n# note\nb: !and\n- x\nc: 2\n", ""},
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
			// and what it wrote says the same thing it was given
			back, err := parse.Parse([]byte(b.String()), parse.ParseComments(true))
			if err != nil {
				t.Fatalf("the output does not parse: %v\n%s", err, b.String())
			}
			if !n.DeepEqualWithComments(back) {
				t.Errorf("the output parses as a different document:\n in %q\nout %q", tc.src, b.String())
			}
		})
	}
}
