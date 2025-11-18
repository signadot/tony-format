package codegen

import (
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

func TestGenerateSchema_WithComments(t *testing.T) {
	src := `package test

// Person represents a person in the system
// This is a multi-line comment
type Person struct {
	schemaTag ` + "`tony:\"schemadef=person\"`" + `
	// Name is the person's full name
	Name string ` + "`tony:\"field=name\"`" + `
	// Age is the person's age in years
	Age  int
	// Email is optional
	Email *string
}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	structs, err := ExtractStructs(file, "test.go")
	if err != nil {
		t.Fatalf("failed to extract structs: %v", err)
	}

	if len(structs) != 1 {
		t.Fatalf("expected 1 struct, got %d", len(structs))
	}

	person := structs[0]

	// Verify comments were extracted
	if len(person.Comments) == 0 {
		t.Fatal("expected struct-level comments")
	}
	if !strings.Contains(strings.Join(person.Comments, " "), "Person represents") {
		t.Errorf("expected comment about Person, got: %v", person.Comments)
	}

	// Verify field comments were extracted
	nameField := person.Fields[0]
	if len(nameField.Comments) == 0 {
		t.Fatal("expected field-level comments for Name")
	}
	if !strings.Contains(strings.Join(nameField.Comments, " "), "Name is") {
		t.Errorf("expected comment about Name, got: %v", nameField.Comments)
	}

	// Generate schema
	schema, err := GenerateSchema(structs, person)
	if err != nil {
		t.Fatalf("failed to generate schema: %v", err)
	}

	// Verify schema structure
	if schema.Type != ir.ObjectType {
		t.Errorf("expected ObjectType, got %v", schema.Type)
	}

	// Find the define map - iterate through Fields/Values to find "define"
	var defineNode *ir.Node
	for i, field := range schema.Fields {
		if field.String == "define" {
			defineNode = schema.Values[i]
			break
		}
	}
	if defineNode == nil {
		t.Fatalf("could not find 'define' in schema. Fields: %v", schema.Fields)
	}
	if defineNode.Type != ir.ObjectType {
		t.Fatalf("expected define to be ObjectType, got %v", defineNode.Type)
	}

	// Find person definition - fields are now directly in define, not nested under "person"
	var personDef *ir.Node
	if defineNode.Type == ir.ObjectType {
		// Fields are directly in define, so personDef is defineNode itself
		personDef = defineNode
	} else {
		// Legacy: look for nested "person" key
		for i, field := range defineNode.Fields {
			if field.String == "person" {
				personDef = defineNode.Values[i]
				break
			}
		}
	}

	if personDef == nil {
		t.Fatal("could not find person definition in schema")
	}

	// Verify struct-level comment is preserved
	if personDef.Comment == nil {
		t.Fatal("expected struct-level comment on person definition")
	}
	if personDef.Comment.Type != ir.CommentType {
		t.Errorf("expected CommentType, got %v", personDef.Comment.Type)
	}
	if len(personDef.Comment.Lines) == 0 {
		t.Fatal("expected comment lines")
	}
	commentText := strings.Join(personDef.Comment.Lines, " ")
	if !strings.Contains(commentText, "Person represents") {
		t.Errorf("expected comment about Person, got: %v", personDef.Comment.Lines)
	}

	// Verify field-level comments are preserved
	var nameFieldNode *ir.Node
	for i, field := range personDef.Fields {
		if field.String == "name" {
			nameFieldNode = personDef.Values[i]
			break
		}
	}

	if nameFieldNode == nil {
		t.Fatal("could not find name field in person definition")
	}

	// Check if comment is on the field type node
	if nameFieldNode.Comment == nil {
		t.Fatal("expected field-level comment on name field type")
	}
	if nameFieldNode.Comment.Type != ir.CommentType {
		t.Errorf("expected CommentType, got %v", nameFieldNode.Comment.Type)
	}
	if len(nameFieldNode.Comment.Lines) == 0 {
		t.Fatal("expected comment lines")
	}
	fieldCommentText := strings.Join(nameFieldNode.Comment.Lines, " ")
	if !strings.Contains(fieldCommentText, "Name is") {
		t.Errorf("expected comment about Name, got: %v", nameFieldNode.Comment.Lines)
	}
}

func TestGenerateSchema_FieldComments(t *testing.T) {
	src := `package test

type Person struct {
	schemaTag ` + "`tony:\"schemadef=person\"`" + `
	// Name comment
	Name string
	// Age comment
	Age int
}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	structs, err := ExtractStructs(file, "test.go")
	if err != nil {
		t.Fatalf("failed to extract structs: %v", err)
	}

	if len(structs) != 1 {
		t.Fatalf("expected 1 struct, got %d", len(structs))
	}

	schema, err := GenerateSchema(structs, structs[0])
	if err != nil {
		t.Fatalf("failed to generate schema: %v", err)
	}

	// Find the define map
	var defineNode *ir.Node
	for i, field := range schema.Fields {
		if field.String == "define" {
			defineNode = schema.Values[i]
			break
		}
	}
	if defineNode == nil {
		t.Fatal("could not find 'define' in schema")
	}

	// Find person definition - fields are now directly in define, not nested under "person"
	var personDef *ir.Node
	// Check if define is an object with fields directly
	if defineNode.Type == ir.ObjectType {
		// Fields are directly in define, so personDef is defineNode itself
		personDef = defineNode
	} else {
		// Legacy: look for nested "person" key
		for i, field := range defineNode.Fields {
			if field.String == "person" {
				personDef = defineNode.Values[i]
				break
			}
		}
	}

	if personDef == nil {
		t.Fatal("could not find person definition")
	}

	// Check Name field comment
	var nameFieldNode *ir.Node
	for i, field := range personDef.Fields {
		if field.String == "Name" {
			nameFieldNode = personDef.Values[i]
			break
		}
	}

	if nameFieldNode == nil {
		t.Fatal("could not find Name field")
	}

	if nameFieldNode.Comment == nil {
		t.Fatal("expected comment on Name field")
	}
	if !strings.Contains(strings.Join(nameFieldNode.Comment.Lines, " "), "Name comment") {
		t.Errorf("expected 'Name comment', got: %v", nameFieldNode.Comment.Lines)
	}

	// Check Age field comment
	var ageFieldNode *ir.Node
	for i, field := range personDef.Fields {
		if field.String == "Age" {
			ageFieldNode = personDef.Values[i]
			break
		}
	}

	if ageFieldNode == nil {
		t.Fatal("could not find Age field")
	}

	if ageFieldNode.Comment == nil {
		t.Fatal("expected comment on Age field")
	}
	if !strings.Contains(strings.Join(ageFieldNode.Comment.Lines, " "), "Age comment") {
		t.Errorf("expected 'Age comment', got: %v", ageFieldNode.Comment.Lines)
	}
}

func TestGenerateSchema_NoComments(t *testing.T) {
	src := `package test

type Person struct {
	schemaTag ` + "`tony:\"schemadef=person\"`" + `
	Name string
	Age  int
}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	structs, err := ExtractStructs(file, "test.go")
	if err != nil {
		t.Fatalf("failed to extract structs: %v", err)
	}

	if len(structs) != 1 {
		t.Fatalf("expected 1 struct, got %d", len(structs))
	}

	schema, err := GenerateSchema(structs, structs[0])
	if err != nil {
		t.Fatalf("failed to generate schema: %v", err)
	}

	// Find the define map
	var defineNode *ir.Node
	for i, field := range schema.Fields {
		if field.String == "define" {
			defineNode = schema.Values[i]
			break
		}
	}
	if defineNode == nil {
		t.Fatal("could not find 'define' in schema")
	}

	// Find person definition - fields are now directly in define
	var personDef *ir.Node
	if defineNode.Type == ir.ObjectType {
		personDef = defineNode
	} else {
		// Legacy: look for nested "person" key
		for i, field := range defineNode.Fields {
			if field.String == "person" {
				personDef = defineNode.Values[i]
				break
			}
		}
	}

	if personDef == nil {
		t.Fatal("could not find person definition")
	}

	// Struct without comments should not have comment node
	if personDef.Comment != nil {
		t.Error("expected no comment on struct without comments")
	}

	// Fields without comments should not have comment nodes
	for i, field := range personDef.Fields {
		fieldNode := personDef.Values[i]
		if fieldNode.Comment != nil {
			t.Errorf("expected no comment on field %s", field.String)
		}
	}
}

// Test that comments are preserved when wrapping optional fields
func TestGenerateSchema_CommentsWithOptionalFields(t *testing.T) {
	src := `package test

type Person struct {
	schemaTag ` + "`tony:\"schemadef=person\"`" + `
	// Email comment
	Email string ` + "`tony:\"optional\"`" + `
}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	structs, err := ExtractStructs(file, "test.go")
	if err != nil {
		t.Fatalf("failed to extract structs: %v", err)
	}

	// Set up reflection types for the struct
	person := structs[0]
	emailField := person.Fields[0]
	emailField.Type = reflect.TypeOf("")

	schema, err := GenerateSchema(structs, person)
	if err != nil {
		t.Fatalf("failed to generate schema: %v", err)
	}

	// Find the define map
	var defineNode *ir.Node
	for i, field := range schema.Fields {
		if field.String == "define" {
			defineNode = schema.Values[i]
			break
		}
	}
	if defineNode == nil {
		t.Fatal("could not find 'define' in schema")
	}

	// Find person definition - fields are now directly in define
	var personDef *ir.Node
	if defineNode.Type == ir.ObjectType {
		personDef = defineNode
	} else {
		// Legacy: look for nested "person" key
		for i, field := range defineNode.Fields {
			if field.String == "person" {
				personDef = defineNode.Values[i]
				break
			}
		}
	}

	if personDef == nil {
		t.Fatal("could not find person definition")
	}

	// Find Email field (should be wrapped in !or)
	var emailFieldNode *ir.Node
	for i, field := range personDef.Fields {
		if field.String == "Email" {
			emailFieldNode = personDef.Values[i]
			break
		}
	}

	if emailFieldNode == nil {
		t.Fatal("could not find Email field")
	}

	// Email should be wrapped in !or [null, string]
	// Check if it's an ArrayType with !or tag, or check the first value
	if emailFieldNode.Type == ir.ArrayType && len(emailFieldNode.Values) > 0 {
		// Check if first value is the !or string
		if len(emailFieldNode.Values) > 0 && emailFieldNode.Values[0].String == "!or" {
			// Good, it's wrapped
		} else {
			t.Errorf("expected !or wrapper, got Type=%v Values=%v", emailFieldNode.Type, emailFieldNode.Values)
		}
	} else {
		t.Errorf("expected ArrayType with !or, got Type=%v Tag=%q Values=%v", emailFieldNode.Type, emailFieldNode.Tag, emailFieldNode.Values)
	}

	// Comment should be preserved on the wrapped node
	if emailFieldNode.Comment == nil {
		t.Fatal("expected comment on wrapped Email field")
	}
	if !strings.Contains(strings.Join(emailFieldNode.Comment.Lines, " "), "Email comment") {
		t.Errorf("expected 'Email comment', got: %v", emailFieldNode.Comment.Lines)
	}
}
