package token

import (
	"errors"
	"testing"
)

// A code point above the BMP arrives from JSON as a surrogate pair, one \u escape
// per half: that is how Python's json.dumps writes every emoji, and tony.md says
// valid JSON is valid tony. Each half was decoded on its own, and a surrogate is
// not a rune, so the pair came out as two U+FFFD (4ynqp7wqh12krg32msn0 item 15).
// A half on its own names nothing and is refused, as a bad hex digit is.
func TestSurrogatePairEscapesDecodeToOneRune(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`"😀"`, "😀"},
		{`"a😀b"`, "a😀b"},
		{`"𝄞"`, "𝄞"},
		{`"é"`, "é"}, // the BMP is unchanged
	} {
		got, err := Unquote(tc.in)
		if err != nil {
			t.Errorf("Unquote(%s): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Unquote(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, bad := range []string{`"\ud83d"`, `"\ud83dx"`, `"\ud83dA"`, `"\ude00"`, `"\ud83d\uzzzz"`} {
		if _, err := Unquote(bad); !errors.Is(err, ErrBadUnicode) {
			t.Errorf("Unquote(%s): err %v, want ErrBadUnicode", bad, err)
		}
	}
	// What the encoder writes for the same rune is the rune itself, and it reads back.
	if q := Quote("😀", false); q != `"😀"` {
		t.Errorf("Quote(😀) = %s", q)
	}
}
