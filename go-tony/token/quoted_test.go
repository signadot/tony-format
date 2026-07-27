package token

import "testing"

func TestQuoted(t *testing.T) {
	for _, s := range []string{
		`"`,
		`'`,
		"\t\n\v\r\b",
		"∞∞",
		`"""''`,
		`''"∞∞""''`,
		`''"∞''∞"\r"''`,
		`f[0]`,
	} {
		do(s, t)
	}
}

func do(v string, t *testing.T) {
	q := Quote(v, true)
	uq, err := Unquote(q)
	if err != nil {
		t.Errorf("error unquoting %q (from %q): %v", q, v, err)
		return
	}
	// Unquote must return the original value. This used to compare against q, the
	// quoted form, which is what Unquote returned when it validated the input and then
	// handed it back undecoded — the assertion held the bug in place.
	if uq != v {
		t.Errorf("Unquote(Quote(%q) = %q) = %q, want %q", v, q, uq, v)
	}
	if NeedsQuote(v) {
		t.Logf("%q needs quote\n", v)
	} else {
		t.Logf("does not need quote: %s\n", v)
	}
}
