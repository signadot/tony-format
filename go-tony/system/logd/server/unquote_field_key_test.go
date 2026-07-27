package server

import "testing"

// Stored field keys that are shaped like a quoted string but are not well-formed. The
// previous shape test (opens with a quote, ends with the same one) admitted all of these
// and handed them to a decoder that panicked, taking the whole daemon down from any path
// resolution that walked past such a key.
var malformedKeys = []string{
	`"a"b"`,
	"\"\\`\"",
	`"\q"`,
	`"a\qb"`,
	"\"x\\`yyyyyyyyyyyy\"",
	`"\—"`,
	`"a`,
	`"`,
	`'`,
	`"\`,
	`'a"b`,
}

func TestUnquoteFieldKeyMalformed(t *testing.T) {
	for _, k := range malformedKeys {
		var panicked any
		var got string
		func() {
			defer func() { panicked = recover() }()
			got = unquoteFieldKey(k)
		}()
		if panicked != nil {
			t.Errorf("unquoteFieldKey(%q) panicked: %v", k, panicked)
			continue
		}
		if got != k {
			t.Errorf("unquoteFieldKey(%q) = %q; a key that is not a well-formed quoted string must come back unchanged", k, got)
		}
	}
}

func TestUnquoteFieldKeyWellFormed(t *testing.T) {
	cases := map[string]string{
		`"abc"`:  "abc",
		`'abc'`:  "abc",
		`"a\"b"`: `a"b`,
		`"a\nb"`: "a\nb",
		`""`:     "",
		`''`:     "",
		// A double quote inside a single-quoted key needs no escape, and vice versa.
		`'a"b'`: `a"b`,
		`"a'b"`: "a'b",
		// Bare keys pass through, including one whose first and last bytes match.
		"abc": "abc",
		"aba": "aba",
		"a":   "a",
		"":    "",
	}
	for in, want := range cases {
		if got := unquoteFieldKey(in); got != want {
			t.Errorf("unquoteFieldKey(%q) = %q, want %q", in, got, want)
		}
	}
}
