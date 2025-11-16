package token

import "testing"

func TestYAMLDoubleQuoted(t *testing.T) {
	var ss = []string{
		`""`,
		`" "`,
		`"\""`,
		`"a b"`,
		`"a
b"`,
		`"a\
  \ b"`,
		`"a\nb"`,
		`"a\

 \ b"`,
		`"\"^(\\+|-)?(([0-9"`,
		`'[]'`,
		`"\
                Name should be a unique inside the OLM\n\
                \n\
                It can be up to 30 characters, consisting of alphanumeric\n\
                characters or '-', but it must both start and end with an alphanumeric\n\
                character.\
"`,
	}

	for _, s := range ss {
		tok, off, err := YAMLQuotedString([]byte(s), nil)
		if err != nil {
			t.Errorf("%q: %v", s, err)
			continue
		}
		if off != len(s) {
			t.Errorf("incorrect offset %d/%d", off, len(s))
			continue
		}
		t.Logf("`%s` gave `%s` (raw `%s`)", s /*tok.String()*/, "n/a", string(tok.Bytes))
	}
}
