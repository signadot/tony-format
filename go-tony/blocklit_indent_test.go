package tony

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
)

// A block literal's content is one level in from the LINE its '|' is on, and each
// '- ' on that line is a level. Two places disagreed about the second half.
//
// The reader counted only the line's leading indent, so `- - |` was read as though
// the markers were not there: its content wanted column 2, which is left of the '|'
// itself, and the two columns between became part of the string.
//
// The encoder counted the element level twice -- once in encodeArray for the '- '
// and again in encodeBlockLit -- so it wrote every array element's content two
// columns too deep. Both together made `o v` change the value of any block literal
// in an array, by two spaces, again on every pass. `o v -w` writes that back over
// the file (crcz1erdh12ks8cvj1n0).
//
// PyYAML reads every case below the same way, which is what settled which side was
// wrong: `- |` + two spaces is "ape\n" to YAML, and `- - |` needs four.
func TestBlockLit_ContentIndent(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string // what the one literal in the document holds
	}{
		{"a field's value", "k: |\n  ape\n", "ape\n"},
		{"an array element", "- |\n  ape\n", "ape\n"},
		{"an array under a field", "a:\n- |\n  ape\n", "ape\n"},
		{"two markers on the line", "- - |\n    ape\n", "ape\n"},
		{"three", "- - - |\n      ape\n", "ape\n"},
		{"bracketed field", "{k: |\n  ape\n}\n", "ape\n"},
		{"bracketed array", "[\n|\n  ape\n]\n", "ape\n"},
		// Indentation past what the level asks for is content, as it is anywhere.
		{"deeper content is content", "- |\n    ape\n", "  ape\n"},
		{"deeper still, nested", "- - |\n      ape\n", "  ape\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc, err := parse.Parse([]byte(test.in))
			if err != nil {
				t.Fatalf("parse: %s", err)
			}
			if got := literalString(t, doc); got != test.want {
				t.Errorf("read %q, want %q", got, test.want)
			}
			// The normal form is a fixed point, and `o v -w` writes it back over
			// the file: a value that shifts by a pass shifts by every pass.
			once, err := encString(doc, false)
			if err != nil {
				t.Fatalf("encode: %s", err)
			}
			again, err := parse.Parse([]byte(once))
			if err != nil {
				t.Fatalf("reparse %q: %s", once, err)
			}
			if got := literalString(t, again); got != test.want {
				t.Errorf("a pass through %q read back %q, want %q", once, got, test.want)
			}
			twice, err := encString(again, false)
			if err != nil {
				t.Fatalf("re-encode: %s", err)
			}
			if twice != once {
				t.Errorf("a second pass wrote\n%q\nafter\n%q", twice, once)
			}
		})
	}
}

// Content dedented past the '|' is not content. The reader used to accept it by
// asking for column 2 whatever the nesting, which is how `- - |` lost two columns.
func TestBlockLit_ContentMayNotOutdentThePipe(t *testing.T) {
	// PyYAML refuses this one outright ("while scanning a simple key").
	doc, err := parse.Parse([]byte("- - |\n  ape\n"))
	if err != nil {
		return // refusing it is the other acceptable answer
	}
	if s := literalString(t, doc); s == "ape\n" {
		t.Errorf("content at column 2 was read as the literal's, under a '|' at column 4")
	}
}
