package token

import "testing"

// TestYAMLDoubleQuotedEscapes: \x, \u and \U are code points in 2, 4 and 8 hex
// digits, and \N, \L and \P are named ones. The scanner read the rune from its
// own output buffer, never stepped over the digits, and ran hex.Encode into a
// one-byte buffer for \x, so `"é"` panicked with an index out of range and
// a longer prefix decoded to garbage; \N and \L were literal panics.
func TestYAMLDoubleQuotedEscapes(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`"é"`, "\"é\""},
		{`"xé\ty"`, "\"xé\ty\""},
		{`"\x41"`, `"A"`},
		{`"\xe9"`, "\"é\""},
		{`"\U0001F600"`, "\"\U0001F600\""},
		{`"\N\L\P"`, "\"  \""},
		{`"é\U0001f600"`, "\"é\U0001F600\""},
	} {
		tok, off, err := YAMLQuotedString([]byte(tc.in), nil)
		if err != nil {
			t.Errorf("%s: %v", tc.in, err)
			continue
		}
		if off != len(tc.in) {
			t.Errorf("%s: consumed %d of %d", tc.in, off, len(tc.in))
		}
		if string(tok.Bytes) != tc.want {
			t.Errorf("%s: got %q, want %q", tc.in, tok.Bytes, tc.want)
		}
	}
	for _, in := range []string{`"\u12"`, `"\xZZ"`, `"\UFFFFFFFF"`, `"\uD800"`} {
		if _, _, err := YAMLQuotedString([]byte(in), nil); err == nil {
			t.Errorf("%s: accepted, want an error", in)
		}
	}
}
