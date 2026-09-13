package encode_test

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

// TestBracketArrayQuotedStringsKeepTheirCommas: in bracketed mode a quoted string
// at the start of a line folds onto the quoted string on the line before it
// (tony.md, multiline folding), so two adjacent quoted elements MUST be separated
// by a comma or they read back as one element. The encoder wrote the comma only
// after a string that was itself folded, and ["a b", "c d"] came back as one
// element "a bc d" -- data lost on every view of a bracketed file.
func TestBracketArrayQuotedStringsKeepTheirCommas(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		opts []encode.EncodeOption
	}{
		{"two quoted", `["a b", "c d"]`, nil},
		{"three with a bare one", `["hello world", "foo bar", "baz"]`, nil},
		{"quoted around a number", `["a b", 1, "c d", "e f"]`, nil},
		{"nested under a key", `{k: ["a b", "c d"]}`, nil},
		{"nested arrays", `[["a b", "c d"], ["e f", "g h"]]`, nil},
		{"empty strings", `["", ""]`, nil},
		{"folded then quoted", "[\n  \"a\"\n  \"b\"\n  \"c d\"\n]", nil},
		{"brackets option", "- \"a b\"\n- \"c d\"\n", []encode.EncodeOption{encode.EncodeBrackets(true)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, err := parse.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("parse src: %v", err)
			}
			var b strings.Builder
			if err := encode.Encode(want, &b, tc.opts...); err != nil {
				t.Fatalf("encode: %v", err)
			}
			got, err := parse.Parse([]byte(b.String()))
			if err != nil {
				t.Fatalf("reparse %q: %v", b.String(), err)
			}
			// Reparsing a bracketed document tags the array !bracket; the
			// elements are the question, not the presentation.
			got = got.WithTag(want.Tag)
			if !got.DeepEqual(want) {
				t.Errorf("round trip changed the document:\n src %q\n out %q\n got %v\nwant %v",
					tc.src, b.String(), got, want)
			}
		})
	}
}
