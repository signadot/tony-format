package codegen

import (
	"go/parser"
	"go/token"
	"reflect"
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
)

func TestExtractStructs_Directives(t *testing.T) {
	src := `
package testpkg

//tony:schemagen=person
//tony:context=tony-format/context
type Person struct {
	Name string
}

// Regular struct without directive
type Other struct {
	ID int
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	structs, err := ExtractTypes(file, "test.go")
	if err != nil {
		t.Fatalf("ExtractStructs failed: %v", err)
	}

	if len(structs) != 1 {
		t.Fatalf("Expected 1 struct, got %d", len(structs))
	}

	s := structs[0]
	if s.Name != "Person" {
		t.Errorf("Expected struct name Person, got %s", s.Name)
	}

	if s.StructSchema == nil {
		t.Fatal("Expected StructSchema to be set from directive")
	}

	if s.StructSchema.SchemaName != "person" {
		t.Errorf("Expected schema name 'person', got '%s'", s.StructSchema.SchemaName)
	}

	if s.StructSchema.Mode != "schemagen" {
		t.Errorf("Expected mode 'schemagen', got '%s'", s.StructSchema.Mode)
	}

	if s.StructSchema.Context != "tony-format/context" {
		t.Errorf("Expected context 'tony-format/context', got '%s'", s.StructSchema.Context)
	}
}

func TestExtractStructs_Directives_Mixed(t *testing.T) {
	// Test that directive works even if mixed with other comments
	src := `
package testpkg

// Person represents a human being.
//
//tony:schemagen=person
//
// Some other comments.
type Person struct {
	Name string
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	structs, err := ExtractTypes(file, "test.go")
	if err != nil {
		t.Fatalf("ExtractStructs failed: %v", err)
	}

	if len(structs) != 1 {
		t.Fatalf("Expected 1 struct, got %d", len(structs))
	}

	s := structs[0]
	if s.StructSchema == nil {
		t.Fatal("Expected StructSchema to be set from directive")
	}

	if s.StructSchema.SchemaName != "person" {
		t.Errorf("Expected schema name 'person', got '%s'", s.StructSchema.SchemaName)
	}
}

// TestExtractStructs_MarkerMatchesDirective: an anonymous schema marker and a
// //tony: directive are two spellings of one annotation. The marker's parse read
// every key the directive's does except notag, so a type marked
// `tony:"schemagen=person,notag"` was still written tagged !person
// (addsgv1yh12kszdxmdn0).
func TestExtractStructs_MarkerMatchesDirective(t *testing.T) {
	const annotation = "schemagen=person,notag,allowExtra,context=ctx,codec=custom,comment=C,lineComment=L,tag=T"
	src := "package testpkg\n\ntype marker struct{}\n\n" +
		"type ByMarker struct {\n\tmarker `tony:\"" + annotation + "\"`\n\tName string\n}\n\n" +
		"//tony:" + annotation + "\ntype ByDirective struct {\n\tName string\n}\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	structs, err := ExtractTypes(file, "test.go")
	if err != nil {
		t.Fatalf("ExtractTypes failed: %v", err)
	}
	got := map[string]*gomap.StructSchema{}
	for _, s := range structs {
		got[s.Name] = s.StructSchema
	}
	byMarker, byDirective := got["ByMarker"], got["ByDirective"]
	if byMarker == nil || byDirective == nil {
		t.Fatalf("schemas: marker %v, directive %v", byMarker, byDirective)
	}
	if !byMarker.NoTag {
		t.Error("notag on the marker was not read")
	}
	if !reflect.DeepEqual(byMarker, byDirective) {
		t.Errorf("the two spellings disagree:\n\tmarker    %+v\n\tdirective %+v", *byMarker, *byDirective)
	}
}
