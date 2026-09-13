package schema

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
)

// A match operator the satisfiability formula does not model is an opaque
// proposition, as !ir is, not an unknown tag: a schema using !glob, !subtree,
// !tag(x), !let, !raw or !pass could not load at all, and !not of one has to stay
// satisfiable. A tag nothing implements, and a patch-only operator, are still
// refused (4ynqp7wqh12krg32msn0 item 20).
func TestSchemaLoadsEveryMatchOperator(t *testing.T) {
	for _, pat := range []string{
		`!glob "a*"`,
		`!subtree {a: 1}`,
		`!tag {name: mine}`,
		`!let {x: 1}`,
		`!raw {a: !delete 1}`,
		`!pass null`,
		`!at(a) 1`,
		`!has-path a`,
		`!not.glob "a*"`,
		`!and [!irtype "", !glob "a*"]`,
	} {
		src := "accept: {v: " + pat + "}\n" // the formula is built for accept
		node, err := parse.Parse([]byte(src))
		if err != nil {
			t.Fatalf("parse %q: %v", pat, err)
		}
		if _, err := ParseSchema(node); err != nil {
			t.Errorf("a schema using %s does not load: %v", pat, err)
		}
	}

	for _, bad := range []struct{ pat, want string }{
		{`!regexp "a+"`, "unknown tag"}, // nothing implements it
		{`!delete null`, "unknown tag"}, // a patch operator, not a match
	} {
		node, err := parse.Parse([]byte("accept: {v: " + bad.pat + "}\n"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = ParseSchema(node)
		if err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("a schema using %s loaded (err %v), want a refusal saying %q", bad.pat, err, bad.want)
		}
	}
}
