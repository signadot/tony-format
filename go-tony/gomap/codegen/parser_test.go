package codegen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestExtractStructs(t *testing.T) {
	src := `package test

// Person represents a person
type Person struct {
	_    ` + "`tony:\"schemagen=person\"`" + `
	// Name is the person's name
	Name string ` + "`tony:\"field=name\"`" + `
	Age  int
}

type User struct {
	_    ` + "`tony:\"schema=user\"`" + `
	ID   string
	Name string
}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	structs, err := ExtractTypes(file, "test.go")
	if err != nil {
		t.Fatalf("failed to extract structs: %v", err)
	}

	if len(structs) != 2 {
		t.Fatalf("expected 2 structs, got %d", len(structs))
	}

	// Check Person struct
	person := structs[0]
	if person.Name != "Person" {
		t.Errorf("expected Person, got %q", person.Name)
	}
	if person.StructSchema == nil {
		t.Fatal("expected StructSchema for Person")
	}
	if person.StructSchema.Mode != "schemagen" {
		t.Errorf("expected mode schemagen, got %q", person.StructSchema.Mode)
	}
	if person.StructSchema.SchemaName != "person" {
		t.Errorf("expected schema name person, got %q", person.StructSchema.SchemaName)
	}
	if len(person.Comments) == 0 {
		t.Error("expected comments for Person")
	}
	if len(person.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(person.Fields))
	}

	// Check Person fields
	nameField := person.Fields[0]
	if nameField.Name != "Name" {
		t.Errorf("expected field Name, got %q", nameField.Name)
	}
	if nameField.SchemaFieldName != "name" {
		t.Errorf("expected schema field name 'name', got %q", nameField.SchemaFieldName)
	}
	if len(nameField.Comments) == 0 {
		t.Error("expected comments for Name field")
	}

	// Check User struct
	user := structs[1]
	if user.Name != "User" {
		t.Errorf("expected User, got %q", user.Name)
	}
	if user.StructSchema == nil {
		t.Fatal("expected StructSchema for User")
	}
	if user.StructSchema.Mode != "schema" {
		t.Errorf("expected mode schema, got %q", user.StructSchema.Mode)
	}
	if user.StructSchema.SchemaName != "user" {
		t.Errorf("expected schema name user, got %q", user.StructSchema.SchemaName)
	}
}

func TestExtractComments(t *testing.T) {
	src := `package test

// First comment
// Second comment
type Person struct {
	_ ` + "`tony:\"schemagen=person\"`" + `
}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	var genDecl *ast.GenDecl
	for _, decl := range file.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok {
			genDecl = gd
			break
		}
	}

	if genDecl == nil {
		t.Fatal("expected GenDecl")
	}

	comments := ExtractComments(genDecl)
	if len(comments) < 2 {
		t.Errorf("expected at least 2 comments, got %d", len(comments))
	}
	// Verify comments have "# " prefix (Tony format)
	for i, comment := range comments {
		if !strings.HasPrefix(comment, "# ") {
			t.Errorf("comment %d should start with \"# \", got: %q", i, comment)
		}
	}
}

func TestExtractFields_Omitzero(t *testing.T) {
	src := `package test

type Event struct {
	_        ` + "`tony:\"schemagen=event\"`" + `
	Name     string ` + "`tony:\"field=name\"`" + `
	Complete bool   ` + "`tony:\"field=complete,omitzero\"`" + `
	Required bool   ` + "`tony:\"field=required\"`" + `
}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	structs, err := ExtractTypes(file, "test.go")
	if err != nil {
		t.Fatalf("failed to extract structs: %v", err)
	}

	if len(structs) != 1 {
		t.Fatalf("expected 1 struct, got %d", len(structs))
	}

	event := structs[0]
	if len(event.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(event.Fields))
	}

	// Check Name field (no omitzero)
	nameField := event.Fields[0]
	if nameField.Name != "Name" {
		t.Errorf("expected Name, got %q", nameField.Name)
	}
	if nameField.Omitzero {
		t.Error("Name field should not have omitzero")
	}

	// Check Complete field (has omitzero)
	completeField := event.Fields[1]
	if completeField.Name != "Complete" {
		t.Errorf("expected Complete, got %q", completeField.Name)
	}
	if completeField.SchemaFieldName != "complete" {
		t.Errorf("expected schema field name 'complete', got %q", completeField.SchemaFieldName)
	}
	if !completeField.Omitzero {
		t.Error("Complete field should have omitzero")
	}

	// Check Required field (no omitzero)
	requiredField := event.Fields[2]
	if requiredField.Name != "Required" {
		t.Errorf("expected Required, got %q", requiredField.Name)
	}
	if requiredField.Omitzero {
		t.Error("Required field should not have omitzero")
	}
}

func TestExtractTypes_TypeDefinition(t *testing.T) {
	src := `package test

// Person is a base struct
type Person struct {
	Name string
	Age  int
}

// tony:schemagen=employee
type Employee Person
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	structs, err := ExtractTypes(file, "test.go")
	if err != nil {
		t.Fatalf("failed to extract types: %v", err)
	}

	// Only Employee should be extracted (has schema directive)
	if len(structs) != 1 {
		t.Fatalf("expected 1 type with schema, got %d", len(structs))
	}

	employee := structs[0]
	if employee.Name != "Employee" {
		t.Errorf("expected Employee, got %q", employee.Name)
	}
	if employee.StructSchema == nil {
		t.Fatal("expected StructSchema for Employee")
	}
	if employee.StructSchema.Mode != "schemagen" {
		t.Errorf("expected mode schemagen, got %q", employee.StructSchema.Mode)
	}
	if employee.StructSchema.SchemaName != "employee" {
		t.Errorf("expected schema name employee, got %q", employee.StructSchema.SchemaName)
	}

	// Employee should have fields from Person
	if len(employee.Fields) != 2 {
		t.Fatalf("expected 2 fields inherited from Person, got %d", len(employee.Fields))
	}

	// Check fields
	if employee.Fields[0].Name != "Name" {
		t.Errorf("expected first field Name, got %q", employee.Fields[0].Name)
	}
	if employee.Fields[1].Name != "Age" {
		t.Errorf("expected second field Age, got %q", employee.Fields[1].Name)
	}
}
