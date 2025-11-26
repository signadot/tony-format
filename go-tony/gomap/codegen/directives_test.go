package codegen

import (
	"go/parser"
	"go/token"
	"testing"
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
