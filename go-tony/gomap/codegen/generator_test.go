package codegen

import (
	"reflect"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/schema"
)

func TestGenerateToTonyMethod_SimpleStruct(t *testing.T) {
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

	code, err := GenerateToTonyMethod(structInfo, s)
	if err != nil {
		t.Fatalf("GenerateToTonyMethod failed: %v", err)
	}

	// Check that code contains expected elements
	if !strings.Contains(code, "func (s *Person) ToTony(opts ...encode.EncodeOption)") {
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
	if !strings.Contains(code, `node.Tag = "!person"`) {
		t.Errorf("Expected schema tag, got:\n%s", code)
	}
}

func TestGenerateToTonyMethod_OptionalField(t *testing.T) {
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

	code, err := GenerateToTonyMethod(structInfo, s)
	if err != nil {
		t.Fatalf("GenerateToTonyMethod failed: %v", err)
	}

	// Check that optional field has nil check
	if !strings.Contains(code, "if s.Email != nil") {
		t.Errorf("Expected nil check for optional Email field, got:\n%s", code)
	}
}

func TestGenerateToTonyMethod_SliceField(t *testing.T) {
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

	code, err := GenerateToTonyMethod(structInfo, s)
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

func TestGenerateToTonyMethod_MapField(t *testing.T) {
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

	code, err := GenerateToTonyMethod(structInfo, s)
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

func TestGenerateFromTonyMethod_SimpleStruct(t *testing.T) {
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

	code, err := GenerateFromTonyMethod(structInfo, s)
	if err != nil {
		t.Fatalf("GenerateFromTonyMethod failed: %v", err)
	}

	// Check that code contains expected elements
	if !strings.Contains(code, "func (s *Person) FromTony(node *ir.Node, opts ...parse.ParseOption) error") {
		t.Errorf("Expected FromTony method signature, got:\n%s", code)
	}
	if !strings.Contains(code, "node.Type != ir.ObjectType") {
		t.Errorf("Expected type validation, got:\n%s", code)
	}
	if !strings.Contains(code, `ir.Get(node, "name")`) {
		t.Errorf("Expected name field extraction, got:\n%s", code)
	}
	if !strings.Contains(code, `ir.Get(node, "age")`) {
		t.Errorf("Expected age field extraction, got:\n%s", code)
	}
}

func TestGenerateFromTonyMethod_RequiredField(t *testing.T) {
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

	code, err := GenerateFromTonyMethod(structInfo, s)
	if err != nil {
		t.Fatalf("GenerateFromTonyMethod failed: %v", err)
	}

	// Check that required field has validation
	if !strings.Contains(code, `required field`) && !strings.Contains(code, `is missing`) {
		t.Errorf("Expected required field validation, got:\n%s", code)
	}
}

func TestGenerateFromTonyMethod_SliceField(t *testing.T) {
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

	code, err := GenerateFromTonyMethod(structInfo, s)
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

func TestGenerateToTonyBytesMethod(t *testing.T) {
	structInfo := &StructInfo{
		Name: "Person",
	}

	code, err := GenerateToTonyBytesMethod(structInfo)
	if err != nil {
		t.Fatalf("GenerateToTonyBytesMethod failed: %v", err)
	}

	if !strings.Contains(code, "func (s *Person) ToTonyBytes(opts ...encode.EncodeOption) ([]byte, error)") {
		t.Errorf("Expected ToTonyBytes signature, got:\n%s", code)
	}
	if !strings.Contains(code, "s.ToTony(opts...)") {
		t.Errorf("Expected call to ToTony, got:\n%s", code)
	}
	if !strings.Contains(code, "encode.Encode(node, &buf, opts...)") {
		t.Errorf("Expected call to encode.Encode, got:\n%s", code)
	}
}

func TestGenerateFromTonyBytesMethod(t *testing.T) {
	structInfo := &StructInfo{
		Name: "Person",
	}

	code, err := GenerateFromTonyBytesMethod(structInfo)
	if err != nil {
		t.Fatalf("GenerateFromTonyBytesMethod failed: %v", err)
	}

	if !strings.Contains(code, "func (s *Person) FromTonyBytes(data []byte, opts ...parse.ParseOption) error") {
		t.Errorf("Expected FromTonyBytes signature, got:\n%s", code)
	}
	if !strings.Contains(code, "parse.Parse(data, opts...)") {
		t.Errorf("Expected call to parse.Parse, got:\n%s", code)
	}
	if !strings.Contains(code, "s.FromTony(node, opts...)") {
		t.Errorf("Expected call to FromTony, got:\n%s", code)
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
	if !strings.Contains(code, "func (s *Person) ToTony(opts ...encode.EncodeOption)") {
		t.Errorf("Expected ToTony method, got:\n%s", code)
	}
	if !strings.Contains(code, "func (s *Person) FromTony(node *ir.Node, opts ...parse.ParseOption) error") {
		t.Errorf("Expected FromTony method, got:\n%s", code)
	}
	if !strings.Contains(code, "func (s *Person) ToTonyBytes(opts ...encode.EncodeOption) ([]byte, error)") {
		t.Errorf("Expected ToTonyBytes method, got:\n%s", code)
	}
	if !strings.Contains(code, "func (s *Person) FromTonyBytes(data []byte, opts ...parse.ParseOption) error") {
		t.Errorf("Expected FromTonyBytes method, got:\n%s", code)
	}

	// Check that code is formatted (should not have obvious formatting issues)
	if strings.Contains(code, "\t\t\t\t") {
		t.Errorf("Code appears to have excessive indentation:\n%s", code)
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
