package codegen

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// A //tony:schemagen on a type that is not a struct is refused where the
// directive is read, before anything is written: it generated code that did not
// compile after the schema file had been updated beside the stale one
// (4ynqp7wqh12krg32msn0 item 25). A type with codec=custom is the exception.
func TestSchemagenOnANonStructIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		refused   bool
	}{
		{"int", "//tony:schemagen=count\ntype Count int\n", true},
		{"map", "//tony:schemagen=sparse\ntype Sparse map[uint32]string\n", true},
		{"custom codec", "//tony:schemagen=m,codec=custom\ntype M map[string]any\n", false},
		{"struct", "//tony:schemagen=p\ntype P struct{ A int }\n", false},
		{"over a struct", "type Q struct{ A int }\n//tony:schemagen=r\ntype R Q\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "t.go", "package p\n\n"+tc.src, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			_, err = ExtractTypes(file, "t.go")
			if tc.refused {
				if err == nil || !strings.Contains(err.Error(), "not a struct") || !strings.Contains(err.Error(), "codec=custom") {
					t.Fatalf("got %v, want a refusal naming the type, saying the directive is for structs, and pointing at codec=custom", err)
				}
			} else if err != nil {
				t.Fatalf("refused: %v", err)
			}
		})
	}
}
