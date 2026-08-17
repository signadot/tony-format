package schema

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
)

// `foo` and `foo(t)` are two definitions, and the satisfiability check indexed
// them by base name -- one entry, whichever the map iteration wrote last.  Which
// body got checked was therefore a coin flip per parse, in both directions: a
// schema whose parameterized definition nothing can satisfy passed about half the
// time, and a sound one was rejected as often, in the same binary on the same
// input.  Issue bsbxmm0th12kshdwg9n0.
func TestDefinitionVariantsAreDistinct(t *testing.T) {
	// only foo(t) is referenced, and it is satisfiable; the plain foo is not,
	// and is not reachable from accept
	sound := `define:
  foo: !and
  - .[string]
  - .[number]
  foo(t): .[string]
accept: .[foo(int)]
`
	// the mirror: the parameterized variant is the unsatisfiable one
	unsound := `define:
  bar: .[string]
  bar(t): !and
  - .[string]
  - .[number]
accept: .[bar(int)]
`
	// a reference by tag carries its arguments too
	byTag := `define:
  baz: .[string]
  baz(t): !and
  - .[string]
  - .[number]
accept: !baz(int) null
`
	// and a schema which mentions both variants has both checked
	both := `define:
  qux: !and
  - .[string]
  - .[number]
  qux(t): .[string]
accept: !and
- .[qux(int)]
- .[qux]
`

	// The same schema, parsed many times: map iteration order is what used to
	// decide the answer, so once is not a test.
	for i := 0; i < 50; i++ {
		if err := parseWithBase(t, sound); err != nil {
			t.Fatalf("a satisfiable schema was rejected: %s", err)
		}
		if err := parseWithBase(t, unsound); err == nil {
			t.Fatal("a schema whose definition nothing can satisfy was accepted")
		}
		if err := parseWithBase(t, byTag); err == nil {
			t.Fatal("a tag reference to an unsatisfiable definition was accepted")
		}
		if err := parseWithBase(t, both); err == nil {
			t.Fatal("the unsatisfiable half of a referenced pair was not checked")
		}
	}
}

func parseWithBase(t *testing.T, src string) error {
	t.Helper()
	node, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse schema: %s", err)
	}
	if err := MergeBaseDefinitions(node); err != nil {
		t.Fatalf("base definitions: %s", err)
	}
	_, err = ParseSchema(node)
	return err
}
