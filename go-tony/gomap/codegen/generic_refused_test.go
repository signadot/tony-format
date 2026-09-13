package codegen

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// A //tony: directive on a generic type is refused where it is read: the
// generator wrote `func (s *Box)` for `type Box[T any]`, which does not compile
// (p478tacqh12krg32msn0 item 25).
func TestDirectiveOnAGenericTypeIsRefused(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "t.go", "package p\n\n//tony:schemagen=box\ntype Box[T any] struct{ V T }\n", parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExtractTypes(file, "t.go")
	if err == nil || !strings.Contains(err.Error(), "Box") || !strings.Contains(err.Error(), "type parameters") {
		t.Fatalf("got %v, want a refusal naming Box and its type parameters", err)
	}
}
