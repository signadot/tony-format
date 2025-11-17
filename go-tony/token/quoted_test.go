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
	if uq != q {
		t.Errorf("unquote(quote(%q)) = %q", v, q)
	}
	if NeedsQuote(v) {
		t.Logf("%q needs quote\n", v)
	} else {
		t.Logf("does not need quote: %s\n", v)
	}
}
