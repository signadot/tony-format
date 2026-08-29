package tony

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// A place inside the document is only a place if the document can hold one.
//
// An object patch reaches the object merge whatever the document's type is --
// doPatchWith switches on the PATCH -- so a scalar document arrives at absentAt, its
// empty Fields making it behave as an empty object, and the placeholder was left
// standing at "field a of a number". Nothing can be at a field of a number:
// ir.Node.Path() says so by panicking, which is the right thing for an assertion to
// do, and it fired from inside the error message !rename builds for a non-object --
// so a patch that was going to be REFUSED took the process down instead
// (kbkxf53ph12krswpj9n0).
//
// The placeholder keeps its parent link, which is load-bearing: an operator asking
// which document it is in has nothing else to ask (p4tzbzx7h12kr6tkhxn0). It stands
// where doc stands instead, which is where an object patch over a scalar puts its
// result anyway.
func TestAPatchOverAScalarIsRefusedNotFatal(t *testing.T) {
	for _, test := range []struct {
		name string
		doc  *ir.Node
		want string
	}{
		{"null document", ir.Null(), "non-object"},
		{"number document", ir.FromInt(1), "non-object"},
		{"object document", mustParse(t, `{}`), "non-object"},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("a refusal killed the process: %v", r)
				}
			}()
			_, err := Patch(test.doc, mustParse(t, `{a: !rename [{from: "x", to: "y"}]}`))
			if err == nil {
				t.Fatal("renaming a field of nothing was accepted")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("refusal does not say why: %v", err)
			}
		})
	}
}

// The placeholder still knows which document it is in when the document is a scalar:
// it stands in the scalar's own place, so the walk up from it is the document's.
func TestTheAbsentPlaceholderStandsWhereAScalarStands(t *testing.T) {
	root := mustParse(t, `{a: 1}`)
	scalar := root.Values[0]

	absent := absentAt(scalar, "gone", 0)
	if absent.Parent != root {
		t.Errorf("the placeholder's parent is %v; a scalar holds nothing, so it stands "+
			"where the scalar does", absent.Parent)
	}
	if absent.Root() != root {
		t.Error("an operator asking which document the placeholder is in cannot be told")
	}
	if got := absent.Path(); got != "$.a" {
		t.Errorf("the placeholder is at %s, want $.a -- the scalar's own place", got)
	}
}
