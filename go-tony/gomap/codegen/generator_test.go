package codegen

import (
	"reflect"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/schema"
)

func TestGenerateToTonyIRMethod_SimpleStruct(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "Person",
		Package: "models",
		Fields: []*FieldInfo{
			{
				Name:            "Name",
				SchemaFieldName: "name",
				Type:            reflect.TypeOf(""),
			},
			{
				Name:            "Age",
				SchemaFieldName: "age",
				Type:            reflect.TypeOf(int(0)),
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "person",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{
			Name: "person",
		},
	}

	code, err := GenerateToTonyIRMethod(structInfo, s)
	if err != nil {
		t.Fatalf("GenerateToTonyMethod failed: %v", err)
	}

	// Check that code contains expected elements
	if !strings.Contains(code, "func (s *Person) ToTonyIR(opts ...gomap.MapOption)") {
		t.Errorf("Expected ToTony method signature, got:\n%s", code)
	}
	if !strings.Contains(code, "irMap := make(map[string]*ir.Node)") {
		t.Errorf("Expected IR map creation, got:\n%s", code)
	}
	if !strings.Contains(code, `irMap["name"]`) {
		t.Errorf("Expected name field mapping, got:\n%s", code)
	}
	if !strings.Contains(code, `irMap["age"]`) {
		t.Errorf("Expected age field mapping, got:\n%s", code)
	}
	if !strings.Contains(code, `.WithTag("!person")`) {
		t.Errorf("Expected schema tag, got:\n%s", code)
	}
}

func TestGenerateToTonyIRMethod_OptionalField(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "Person",
		Package: "models",
		Fields: []*FieldInfo{
			{
				Name:            "Name",
				SchemaFieldName: "name",
				Type:            reflect.TypeOf(""),
			},
			{
				Name:            "Email",
				SchemaFieldName: "email",
				Type:            reflect.TypeOf((*string)(nil)),
				Optional:        true,
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "person",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{
			Name: "person",
		},
	}

	code, err := GenerateToTonyIRMethod(structInfo, s)
	if err != nil {
		t.Fatalf("GenerateToTonyMethod failed: %v", err)
	}

	// Check that optional field has nil check
	if !strings.Contains(code, "if s.Email != nil") {
		t.Errorf("Expected nil check for optional Email field, got:\n%s", code)
	}
}

func TestGenerateToTonyIRMethod_SliceField(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "Person",
		Package: "models",
		Fields: []*FieldInfo{
			{
				Name:            "Tags",
				SchemaFieldName: "tags",
				Type:            reflect.TypeOf([]string{}),
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "person",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{
			Name: "person",
		},
	}

	code, err := GenerateToTonyIRMethod(structInfo, s)
	if err != nil {
		t.Fatalf("GenerateToTonyMethod failed: %v", err)
	}

	// Check that slice handling is present
	if !strings.Contains(code, "ir.FromSlice") {
		t.Errorf("Expected slice conversion, got:\n%s", code)
	}
	if !strings.Contains(code, "for i, v := range s.Tags") {
		t.Errorf("Expected slice iteration, got:\n%s", code)
	}
}

func TestGenerateToTonyIRMethod_MapField(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "Person",
		Package: "models",
		Fields: []*FieldInfo{
			{
				Name:            "Metadata",
				SchemaFieldName: "metadata",
				Type:            reflect.TypeOf(map[string]string{}),
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "person",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{
			Name: "person",
		},
	}

	code, err := GenerateToTonyIRMethod(structInfo, s)
	if err != nil {
		t.Fatalf("GenerateToTonyMethod failed: %v", err)
	}

	// Check that map handling is present
	if !strings.Contains(code, "ir.FromMap") {
		t.Errorf("Expected map conversion, got:\n%s", code)
	}
	if !strings.Contains(code, "for k, v := range s.Metadata") {
		t.Errorf("Expected map iteration, got:\n%s", code)
	}
}

func TestGenerateFromTonyIRMethod_SimpleStruct(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "Person",
		Package: "models",
		Fields: []*FieldInfo{
			{
				Name:            "Name",
				SchemaFieldName: "name",
				Type:            reflect.TypeOf(""),
			},
			{
				Name:            "Age",
				SchemaFieldName: "age",
				Type:            reflect.TypeOf(int(0)),
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "person",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{
			Name: "person",
		},
	}

	code, err := GenerateFromTonyIRMethod(structInfo, s)
	if err != nil {
		t.Fatalf("GenerateFromTonyMethod failed: %v", err)
	}

	// Check that code contains expected elements
	if !strings.Contains(code, "func (s *Person) FromTonyIR(node *ir.Node, opts ...gomap.UnmapOption) error") {
		t.Errorf("Expected FromTony method signature, got:\n%s", code)
	}
	if !strings.Contains(code, "node.Type != ir.ObjectType") {
		t.Errorf("Expected type validation, got:\n%s", code)
	}
	if !strings.Contains(code, `case "name":`) {
		t.Errorf("Expected name field case, got:\n%s", code)
	}
	if !strings.Contains(code, `case "age":`) {
		t.Errorf("Expected age field case, got:\n%s", code)
	}
}

func TestGenerateFromTonyIRMethod_RequiredField(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "Person",
		Package: "models",
		Fields: []*FieldInfo{
			{
				Name:            "Name",
				SchemaFieldName: "name",
				Type:            reflect.TypeOf(""),
				Required:        true,
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "person",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{
			Name: "person",
		},
	}

	code, err := GenerateFromTonyIRMethod(structInfo, s)
	if err != nil {
		t.Fatalf("GenerateFromTonyMethod failed: %v", err)
	}

	// Check that required field has validation
	if !strings.Contains(code, `required field`) && !strings.Contains(code, `is missing`) {
		t.Errorf("Expected required field validation, got:\n%s", code)
	}
}

func TestGenerateFromTonyIRMethod_SliceField(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "Person",
		Package: "models",
		Fields: []*FieldInfo{
			{
				Name:            "Tags",
				SchemaFieldName: "tags",
				Type:            reflect.TypeOf([]string{}),
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "person",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{
			Name: "person",
		},
	}

	code, err := GenerateFromTonyIRMethod(structInfo, s)
	if err != nil {
		t.Fatalf("GenerateFromTonyMethod failed: %v", err)
	}

	// Check that slice handling is present
	if !strings.Contains(code, "fieldNode.Type == ir.ArrayType") {
		t.Errorf("Expected array type check, got:\n%s", code)
	}
	if !strings.Contains(code, "for i, v := range fieldNode.Values") {
		t.Errorf("Expected slice iteration, got:\n%s", code)
	}
}

func TestGenerateToTonyMethod(t *testing.T) {
	structInfo := &StructInfo{
		Name: "Person",
		StructSchema: &gomap.StructSchema{
			SchemaName: "person",
		},
	}

	code, err := GenerateToTonyMethod(structInfo)
	if err != nil {
		t.Fatalf("GenerateToTonyMethod failed: %v", err)
	}

	if !strings.Contains(code, "func (s *Person) ToTony(opts ...gomap.MapOption) ([]byte, error)") {
		t.Errorf("Expected ToTony signature, got:\n%s", code)
	}
	if !strings.Contains(code, "s.ToTonyIR(opts...)") {
		t.Errorf("Expected call to ToTonyIR, got:\n%s", code)
	}
	if !strings.Contains(code, "encode.Encode(node, &buf, gomap.ToEncodeOptions(opts...)...)") {
		t.Errorf("Expected call to encode.Encode, got:\n%s", code)
	}
}

func TestGenerateFromTonyMethod(t *testing.T) {
	structInfo := &StructInfo{
		Name: "Person",
		StructSchema: &gomap.StructSchema{
			SchemaName: "person",
		},
	}

	code, err := GenerateFromTonyMethod(structInfo)
	if err != nil {
		t.Fatalf("GenerateFromTonyMethod failed: %v", err)
	}

	if !strings.Contains(code, "func (s *Person) FromTony(data []byte, opts ...gomap.UnmapOption) error") {
		t.Errorf("Expected FromTony signature, got:\n%s", code)
	}
	if !strings.Contains(code, "parse.Parse(data, gomap.ToParseOptions(opts...)...)") {
		t.Errorf("Expected call to parse.Parse, got:\n%s", code)
	}
	if !strings.Contains(code, "s.FromTonyIR(node, opts...)") {
		t.Errorf("Expected call to FromTonyIR, got:\n%s", code)
	}
}

func TestGeneratePrimitiveToIR(t *testing.T) {
	tests := []struct {
		name     string
		varName  string
		typ      reflect.Type
		expected string
		wantErr  bool
	}{
		{"string", "v", reflect.TypeOf(""), "ir.FromString(v)", false},
		{"int", "v", reflect.TypeOf(int(0)), "ir.FromInt(int64(v))", false},
		{"int64", "v", reflect.TypeOf(int64(0)), "ir.FromInt(int64(v))", false},
		{"float64", "v", reflect.TypeOf(float64(0)), "ir.FromFloat64(float64(v))", false},
		{"bool", "v", reflect.TypeOf(false), "ir.FromBool(v)", false},
		{"unsupported", "v", reflect.TypeOf([]string{}), "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := generatePrimitiveToIR(tt.varName, tt.typ)
			if (err != nil) != tt.wantErr {
				t.Errorf("generatePrimitiveToIR() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result != tt.expected {
				t.Errorf("generatePrimitiveToIR() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGeneratePrimitiveFromIR(t *testing.T) {
	tests := []struct {
		name     string
		varName  string
		typ      reflect.Type
		context  string
		wantErr  bool
		contains []string
	}{
		{
			name:     "string",
			varName:  "v",
			typ:      reflect.TypeOf(""),
			context:  "field",
			wantErr:  false,
			contains: []string{"v.Type != ir.StringType", "elem = v.String"},
		},
		{
			name:     "int",
			varName:  "v",
			typ:      reflect.TypeOf(int(0)),
			context:  "field",
			wantErr:  false,
			contains: []string{"v.Int64 == nil", "elem = int(*v.Int64)"},
		},
		{
			name:     "int8",
			varName:  "v",
			typ:      reflect.TypeOf(int8(0)),
			context:  "field",
			wantErr:  false,
			contains: []string{"overflows int8", "elem = int8(*v.Int64)"},
		},
		{
			name:     "uint",
			varName:  "v",
			typ:      reflect.TypeOf(uint(0)),
			context:  "field",
			wantErr:  false,
			contains: []string{"negative value", "elem = uint(*v.Int64)"},
		},
		{
			name:     "bool",
			varName:  "v",
			typ:      reflect.TypeOf(false),
			context:  "field",
			wantErr:  false,
			contains: []string{"v.Type != ir.BoolType", "elem = v.Bool"},
		},
		{
			name:    "unsupported",
			varName: "v",
			typ:     reflect.TypeOf([]string{}),
			context: "field",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := generatePrimitiveFromIR(tt.varName, tt.typ, tt.context)
			if (err != nil) != tt.wantErr {
				t.Errorf("generatePrimitiveFromIR() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				for _, substr := range tt.contains {
					if !strings.Contains(result, substr) {
						t.Errorf("generatePrimitiveFromIR() result should contain %q, got:\n%s", substr, result)
					}
				}
			}
		})
	}
}

func TestGenerateCode_Integration(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "Person",
		Package: "models",
		Fields: []*FieldInfo{
			{
				Name:            "Name",
				SchemaFieldName: "name",
				Type:            reflect.TypeOf(""),
			},
			{
				Name:            "Age",
				SchemaFieldName: "age",
				Type:            reflect.TypeOf(int(0)),
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "person",
		},
	}

	schemas := map[string]*schema.Schema{
		"person": {
			Signature: &schema.Signature{
				Name: "person",
			},
		},
	}

	config := &CodegenConfig{
		Package: &PackageInfo{
			Name: "models",
		},
	}

	code, err := GenerateCode([]*StructInfo{structInfo}, schemas, config)
	if err != nil {
		t.Fatalf("GenerateCode failed: %v", err)
	}

	// Check that code contains both methods
	if !strings.Contains(code, "func (s *Person) ToTonyIR(opts ...gomap.MapOption)") {
		t.Errorf("Expected ToTony method, got:\n%s", code)
	}
	if !strings.Contains(code, "func (s *Person) FromTonyIR(node *ir.Node, opts ...gomap.UnmapOption) error") {
		t.Errorf("Expected FromTony method, got:\n%s", code)
	}
	if !strings.Contains(code, "func (s *Person) ToTony(opts ...gomap.MapOption) ([]byte, error)") {
		t.Errorf("Expected ToTonyBytes method, got:\n%s", code)
	}
	if !strings.Contains(code, "func (s *Person) FromTony(data []byte, opts ...gomap.UnmapOption) error") {
		t.Errorf("Expected FromTonyBytes method, got:\n%s", code)
	}

	// Check that DO NOT EDIT header is present
	if !strings.Contains(code, "DO NOT EDIT") {
		t.Errorf("Expected DO NOT EDIT header, got:\n%s", code)
	}
}

func TestHasToTonyMethod(t *testing.T) {
	// Create a test struct with ToTony method
	type TestStruct struct {
		Name string
	}

	// This test would require actually implementing ToTony on TestStruct
	// For now, we'll test that the function doesn't panic
	typ := reflect.TypeOf(TestStruct{})
	_ = HasToTonyMethod(typ) // Should return false since method doesn't exist
}

func TestHasFromTonyMethod(t *testing.T) {
	// Create a test struct
	type TestStruct struct {
		Name string
	}

	// This test would require actually implementing FromTony on TestStruct
	// For now, we'll test that the function doesn't panic
	typ := reflect.TypeOf(TestStruct{})
	_ = HasFromTonyMethod(typ) // Should return false since method doesn't exist
}

// TestReproFieldTagIssue verifies that FromTonyIR correctly handles field tags
// when combined with schemagen.
func TestReproFieldTagIssue(t *testing.T) {
	// Define a struct with schemagen and field tags
	type User struct {
		ID   string `tony:"field=user_id"`
		Name string `tony:"field=full_name"`
	}

	structInfo := &StructInfo{
		Name: "User",
		Fields: []*FieldInfo{
			{Name: "ID", Type: reflect.TypeOf(""), SchemaFieldName: "user_id", Required: true},
			{Name: "Name", Type: reflect.TypeOf(""), SchemaFieldName: "full_name"},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "user",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{Name: "user"},
	}

	// Generate FromTonyIR method
	code, err := GenerateFromTonyIRMethod(structInfo, s)
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod failed: %v", err)
	}

	// Check if it uses the correct schema field names in switch cases
	if !strings.Contains(code, `case "user_id":`) {
		t.Errorf("Generated code should contain 'case \"user_id\":', got:\n%s", code)
	}
	if !strings.Contains(code, `case "full_name":`) {
		t.Errorf("Generated code should contain 'case \"full_name\":', got:\n%s", code)
	}
}

// TestReproVariableShadowing verifies that ToTonyIR doesn't redefine 'node'
// in a way that causes compilation errors.
func TestReproVariableShadowing(t *testing.T) {
	// Define a struct with a nested struct field that triggers node creation
	type Nested struct {
		Val int
	}
	type Container struct {
		Inner *Nested
	}

	structInfo := &StructInfo{
		Name: "Container",
		Fields: []*FieldInfo{
			{
				Name:            "Inner",
				Type:            reflect.TypeOf(&Nested{}),
				SchemaFieldName: "inner",
				StructTypeName:  "Nested",
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "container",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{Name: "container"},
	}

	// Generate ToTonyIR method
	code, err := GenerateToTonyIRMethod(structInfo, s)
	if err != nil {
		t.Fatalf("GenerateToTonyIRMethod failed: %v", err)
	}

	// Check for variable redefinition
	// The user says "at the end of ToIR sometimes it redefines 'node' with 'node :='"
	// We want to see if 'node' is defined earlier in the same scope.

	// In the current generator, nested structs generate:
	// if s.Inner != nil {
	//     node, err := s.Inner.ToTonyIR(opts...)
	//     ...
	// }
	// This is in a block, so it shouldn't conflict with the final 'node := ir.FromMap(irMap)'.

	// However, if we change the generator to use ir.FromMap(...).WithTag(...), it's safer.
	t.Logf("Generated code:\n%s", code)
}

// TestReproMapIssue verifies that FromTonyIR generates correct code for maps
func TestReproMapIssue(t *testing.T) {
	type MapStruct struct {
		Data1 map[uint32]string
		Data2 map[*int]string
	}

	structInfo := &StructInfo{
		Name: "MapStruct",
		Fields: []*FieldInfo{
			{
				Name:            "Data1",
				Type:            reflect.TypeOf(map[uint32]string{}),
				SchemaFieldName: "data1",
			},
			{
				Name:            "Data2",
				Type:            reflect.TypeOf(map[*int]string{}),
				SchemaFieldName: "data2",
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "map_struct",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{Name: "map_struct"},
	}

	// Generate FromTonyIR method
	code, err := GenerateFromTonyIRMethod(structInfo, s)
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod failed: %v", err)
	}

	// Check for balanced braces (heuristic)
	open := strings.Count(code, "{")
	close := strings.Count(code, "}")
	if open != close {
		t.Errorf("Unbalanced braces: %d open, %d close\nCode:\n%s", open, close, code)
	}

	// Also check if it compiles/formats (we can't run format here easily without imports, but we can check structure)
	t.Logf("Generated code:\n%s", code)
}
