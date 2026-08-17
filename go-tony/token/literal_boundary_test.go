package token

import (
	"errors"
	"io"
	"testing"
)

// A scan the buffer cut off has not finished, so whether its brackets balance is
// not known yet -- the closer is in the next read.  Called a bad literal, this
// killed a logd client session every time a literal holding a '{' or a '[' fell
// across a buffer boundary (75g1kbpdh12krs09gdn0).
func TestTruncatedLiteralNeedsMoreData(t *testing.T) {
	tests := []struct {
		d    string
		want error // io.EOF for "needs more data", ErrLiteral for a verdict
		lit  string
	}{
		{d: "abc", want: io.EOF},
		{d: "abc{def", want: io.EOF},
		{d: "abc[def", want: io.EOF},
		{d: "abc{def[ghi", want: io.EOF},
		// the scan stopped inside the buffer, so the verdict is knowable
		{d: "abc{def ", want: ErrLiteral},
		{d: "abc[def ", want: ErrLiteral},
		{d: "abc{def\n", want: ErrLiteral},
		// balanced, and something after it to show the scan ended where it did
		{d: "abc{def} ", lit: "abc{def}"},
		{d: "abc[def] ", lit: "abc[def]"},
		{d: "abc ", lit: "abc"},
	}

	for _, test := range tests {
		lit, err := getSingleLiteralStreaming([]byte(test.d))
		switch {
		case test.want != nil:
			if !errors.Is(err, test.want) {
				t.Errorf("%q: error is %v, want %v", test.d, err, test.want)
			}
		case err != nil:
			t.Errorf("%q: %s", test.d, err)
		case string(lit) != test.lit:
			t.Errorf("%q: literal is %q, want %q", test.d, lit, test.lit)
		}
	}
}
