package stream

import (
	"strings"
	"testing"
)

// A key names one field on the wire as in a file: an object that writes a key
// twice is refused by the reader, since logd reads every wire document through it
// (p478tacqh12krg32msn0 item 3).
func TestDuplicateKeyFromTheWireIsRefused(t *testing.T) {
	for _, src := range []string{"{a: 1, a: 2}", "{0: x, 0: y}", "{o: {a: 1, b: 2, a: 3}}"} {
		dec, err := NewDecoder(strings.NewReader(src), WithBrackets())
		if err != nil {
			t.Fatal(err)
		}
		_, err = ReadDocument(dec)
		if err == nil || !strings.Contains(err.Error(), "duplicate key") {
			t.Errorf("%q: got %v, want a duplicate-key refusal", src, err)
		}
	}
	for _, src := range []string{"{a: {x: 1}, b: {x: 2}}", "[{a: 1}, {a: 2}]"} {
		dec, _ := NewDecoder(strings.NewReader(src), WithBrackets())
		if _, err := ReadDocument(dec); err != nil {
			t.Errorf("%q refused: %v", src, err)
		}
	}
}
