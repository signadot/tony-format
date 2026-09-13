package parse

import (
	"strings"
	"testing"
)

// A sparse array's index is 32 bits wide, and an int key above it is refused
// rather than rounded: read as 64 bits and cast, {4294967296: a, 0: b} kept one
// entry under key 0 (p478tacqh12krg32msn0 item 6).
func TestIntKeyAboveTheIndexRangeIsRefused(t *testing.T) {
	for _, src := range []string{"{4294967296: a, 0: b}", "{18446744073709551615: 1}", "4294967296: a\n"} {
		_, err := Parse([]byte(src))
		if err == nil || !strings.Contains(err.Error(), "bad int key") {
			t.Errorf("%q: got %v, want a refusal of the key", src, err)
		}
	}
	if _, err := Parse([]byte("{4294967295: a, 0: b}")); err != nil {
		t.Errorf("the largest index refused: %v", err)
	}
}
