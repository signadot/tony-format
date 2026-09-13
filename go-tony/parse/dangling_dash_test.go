package parse

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
)

// A '- ' with no value after it is refused in tony, as a ':' with none is, and is a
// null element in YAML mode, as YAML reads it. It was an element of nothing: `-`
// alone read as [], `a: -` as a: [], and in YAML `- a`, `-`, `- c` read as [a, c]
// (p478tacqh12krg32msn0 item 7; docs/tony.md, Key Sets).
func TestDanglingDash(t *testing.T) {
	for _, src := range []string{"-\n", "a: -\n", "-\n-\n", "- a\n-\n- c\n", "a:\n- \n", "- a\n- \n"} {
		_, err := Parse([]byte(src))
		if err == nil || !strings.Contains(err.Error(), "'- ' must be followed by a value") {
			t.Errorf("tony %q: got %v, want the dangling '- ' refused", src, err)
		}
	}
	for _, tc := range []struct{ src, want string }{
		{"-\n", "[null]"},
		{"a: -\n", "{a: [null]}"},
		{"-\n-\n", "[null, null]"},
		{"- a\n-\n- c\n", "[a, null, c]"},
		{"- a\n- \n", "[a, null]"},
	} {
		n, err := Parse([]byte(tc.src), ParseYAML())
		if err != nil {
			t.Errorf("yaml %q: %v", tc.src, err)
			continue
		}
		var b strings.Builder
		_ = encode.Encode(n, &b, encode.EncodeWire(true))
		want, _ := Parse([]byte(tc.want))
		var w strings.Builder
		_ = encode.Encode(want, &w, encode.EncodeWire(true))
		if b.String() != w.String() {
			t.Errorf("yaml %q = %s, want %s", tc.src, b.String(), w.String())
		}
	}
	// A value on the line below the marker is the element, not a dangling marker.
	for _, src := range []string{"-\n  x\n", "- # c\n  x\n", "-\n  a: 1\n"} {
		if _, err := Parse([]byte(src)); err != nil {
			t.Errorf("%q refused: %v", src, err)
		}
	}
}
