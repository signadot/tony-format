package schema

import "testing"

func TestRefEscape(t *testing.T) {
	type test struct {
		in, out string
	}
	tests := []test{
		{in: "123", out: "123"},
		{in: ".zoom", out: "\\.zoom"},
	}
	for i := range tests {
		tst := &tests[i]
		if EscapeRef(tst.in) != tst.out {
			t.Errorf("%s -> %s not %s", tst.in, EscapeRef(tst.in), tst.out)
		}
		if UnescapeRef(EscapeRef(tst.in)) != tst.in {
			t.Errorf("%s", tst.in)
		}
	}

}
