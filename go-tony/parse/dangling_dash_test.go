package parse

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
)

// A '- ' with no value after it is refused in tony, as a ':' with none is, and is a
// null element in YAML mode, as YAML reads it. It was an element of nothing: `-`
// alone read as [], `a: -` as a: [], and in YAML `- a`, `-`, `- c` read as [a, c]
// (p478tacqh12krg32msn0 item 7; docs/tony.md, Implicit Null). A tag is not a value,
// so a '- ' with only a tag after it is refused too; it was dropped, `- !t` reading
// as [] (v5dn9vfbh12krj9xwsn0). A comment on the dash's line is not a value
// either: `- # c` and `- !t # c` read as [] (vhdn9vfbh12krgb4wsn0).
func TestDanglingDash(t *testing.T) {
	for _, src := range []string{"-\n", "a: -\n", "-\n-\n", "- a\n-\n- c\n", "a:\n- \n", "- a\n- \n",
		"- !t\n", "- !t\n- 1\n", "- 1\n- !t\n", "a:\n- !t\n",
		"- # c\n", "- # c\n- 1\n", "- !t # c\n", "- !t # c\n- 1\n", "a:\n- !t # c\n"} {
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
		{"- # c\n", "[null]"},
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
	// A value on the line below the marker is the element, not a dangling marker,
	// and so is a tagged value, on the line or below it. A bracket below it is the
	// element at any column, as it is after a ':' (wddn9vfbh12ksq7nwsn0).
	for _, src := range []string{"-\n  x\n", "- # c\n  x\n", "-\n  a: 1\n", "- !t x\n", "- !t\n  x\n",
		"- !t # c\n  x\n", "- !t x # c\n", "- x # c\n- y\n",
		"- !not\n{\n  a: 1\n}\n", "- !not\n[\n  1\n]\n", "-\n{a: 1}\n", "- !t\n  # c\n[1]\n",
		"a:\n- !not\n{\n  a: 1\n}\n- 2\n"} {
		if _, err := Parse([]byte(src)); err != nil {
			t.Errorf("%q refused: %v", src, err)
		}
	}
	// What the block encoder writes for a tagged flow element reads back.
	for _, src := range []string{"- !not {a: 1}\n", "- !not [1]\n", "- {a: 1}\n"} {
		n, err := Parse([]byte(src))
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		var b strings.Builder
		if err := encode.Encode(n, &b); err != nil {
			t.Fatal(err)
		}
		if _, err := Parse([]byte(b.String())); err != nil {
			t.Errorf("%q encodes as %q, which is refused: %v", src, b.String(), err)
		}
	}
}
