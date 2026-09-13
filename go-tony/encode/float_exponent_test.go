package encode

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A float goes out in the text the parser reads back to the same number, and the
// encoder writes again: Go's "1e+21" reparsed as 1e21 with !exp and was rewritten
// "1e21", so `o v` was not idempotent on a large or small float
// (p478tacqh12krg32msn0 item 8).
func TestFloatExponentTextIsStable(t *testing.T) {
	for _, tc := range []struct {
		f    float64
		want string
	}{
		{1e21, "1e21"}, {1.5e-7, "1.5e-7"}, {-2e100, "-2e100"}, {1e-5, "1e-5"}, {123.5, "123.5"},
	} {
		var b strings.Builder
		if err := Encode(ir.FromFloat(tc.f), &b); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(b.String()); got != tc.want {
			t.Errorf("%v wrote %q, want %q", tc.f, got, tc.want)
		}
	}
	for _, src := range []string{"x: 100000000000000000000000.0\n", "y: 0.000000000001\n", "z: 1e+21\n"} {
		once := viewT(t, src)
		if twice := viewT(t, once); twice != once {
			t.Errorf("%q: not idempotent:\n%s\nthen\n%s", src, once, twice)
		}
	}
}

func viewT(t *testing.T, src string) string {
	t.Helper()
	n, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	var b strings.Builder
	if err := Encode(n, &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
