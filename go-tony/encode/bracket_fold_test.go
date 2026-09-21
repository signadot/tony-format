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

// TestWireArrayQuotedStringsKeepTheirCommas: wire is one line, where two quoted
// strings side by side read back as two -- but only while it stays one line.
// Whatever lays it out again puts them on consecutive lines, where they fold, and
// the spec requires the comma between them in any case. Wire wrote
// ["a b" "c d"], and a five-element array stored through docd came back with two.
// Bare elements have nothing to fold and keep their space.
func TestWireArrayQuotedStringsKeepTheirCommas(t *testing.T) {
	for _, tc := range []struct {
		src, want string
	}{
		{`["a b", "c d"]`, `["a b","c d"]`},
		{`["hello world", "foo bar", "baz"]`, `["hello world","foo bar" baz]`},
		{`["a b", 1, "c d", "e f"]`, `["a b" 1 "c d","e f"]`},
		{`{k: ["a b", "c d"]}`, `{k: ["a b","c d"]}`},
		{`[["a b", "c d"], ["e f", "g h"]]`, `[["a b","c d"] ["e f","g h"]]`},
		{`["", ""]`, `["",""]`},
		{`[a, b]`, `[a b]`},
	} {
		t.Run(tc.src, func(t *testing.T) {
			want, err := parse.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("parse src: %v", err)
			}
			var b strings.Builder
			if err := encode.Encode(want, &b, encode.EncodeWire(true)); err != nil {
				t.Fatalf("encode: %v", err)
			}
			if b.String() != tc.want {
				t.Errorf("wire is %q, want %q", b.String(), tc.want)
			}
			// Laid out over lines, as whatever reformats the wire would: a
			// newline after every quoted element, which is where a fold starts.
			laid := strings.NewReplacer(`",`, "\",\n", `" `, "\"\n").Replace(b.String())
			for _, src := range []string{b.String(), laid} {
				got, err := parse.Parse([]byte(src))
				if err != nil {
					t.Fatalf("reparse %q: %v", src, err)
				}
				got = got.WithTag(want.Tag)
				if !got.DeepEqual(want) {
					t.Errorf("round trip changed the document:\n src %q\n out %q\n got %v\nwant %v",
						tc.src, src, got, want)
				}
			}
		})
	}
}
