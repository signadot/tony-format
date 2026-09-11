package schema

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
)

// base.tony's key(p) says every element of a list has a value at p. It wrote the operator
// !all.hasPath, which is not one -- the formula builder refused it, so every schema using
// .[key(...)] failed to load -- and once it loaded, a bare .[key(name)] read name as an
// undefined variable and instantiated key with "", which every value matches. The path is
// a string, .[key("name")], and a bare word naming no definition is refused
// (2jb7njsxh12ksz5xmdn0).
func TestKeyDefinitionHoldsAListToItsKey(t *testing.T) {
	load := func(src string) *Schema {
		t.Helper()
		n, err := parse.Parse([]byte(src))
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		MergeBaseDefinitions(n)
		s, err := ParseSchema(n)
		if err != nil {
			t.Fatalf("ParseSchema: %v", err)
		}
		return s
	}
	s := load("signature: {name: keyed}\naccept: '.[key(\"name\")]'\n")
	for _, c := range []struct {
		src  string
		want bool
	}{
		{"[{name: a, v: 1}, {name: b}]", true},
		{"[{v: 1}]", false},
		{"[]", true},
	} {
		n, err := parse.Parse([]byte(c.src))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(n); (err == nil) != c.want {
			t.Errorf("Validate(%s) = %v, want accepted=%v", c.src, err, c.want)
		}
	}

	bare := load("signature: {name: keyed}\naccept: .[key(name)]\n")
	n, _ := parse.Parse([]byte("[{v: 1}]"))
	if err := bare.Validate(n); err == nil || !strings.Contains(err.Error(), `key(\"name\")`) && !strings.Contains(err.Error(), `key("name")`) {
		t.Errorf(".[key(name)] validated %v, want a refusal saying the path is a string", err)
	}
}
