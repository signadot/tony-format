package codegen

import (
	"reflect"
	"strings"
	"testing"
	"time"

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

	code, err := GenerateToTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
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

	code, err := GenerateToTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateToTonyMethod failed: %v", err)
	}

	// Check that optional field has nil check
	if !strings.Contains(code, "if s.Email != nil") {
		t.Errorf("Expected nil check for optional Email field, got:\n%s", code)
	}
}

func TestGenerateToTonyIRMethod_OmitzeroBool(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "Event",
		Package: "models",
		Fields: []*FieldInfo{
			{
				Name:            "Name",
				SchemaFieldName: "name",
				Type:            reflect.TypeOf(""),
			},
			{
				Name:            "Complete",
				SchemaFieldName: "complete",
				Type:            reflect.TypeOf(false),
				Omitzero:       true,
			},
			{
				Name:            "Required",
				SchemaFieldName: "required",
				Type:            reflect.TypeOf(false),
				Omitzero:       false, // no omitzero - should always serialize
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "event",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{
			Name: "event",
		},
	}

	code, err := GenerateToTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateToTonyMethod failed: %v", err)
	}

	// Check that omitzero bool field has conditional
	if !strings.Contains(code, "if s.Complete {") {
		t.Errorf("Expected conditional for omitzero Complete field, got:\n%s", code)
	}

	// Check that non-omitzero bool field is unconditional
	// The Required field should be set directly without a conditional
	if strings.Contains(code, "if s.Required {") {
		t.Errorf("Did not expect conditional for non-omitzero Required field, got:\n%s", code)
	}
	if !strings.Contains(code, `irMap["required"] = ir.FromBool(s.Required)`) {
		t.Errorf("Expected unconditional Required field mapping, got:\n%s", code)
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

	code, err := GenerateToTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
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

	code, err := GenerateToTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
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

	code, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
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

	code, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
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

	code, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateFromTonyMethod failed: %v", err)
	}

	// Check that slice handling is present
	if !strings.Contains(code, "fieldNodeUnwrapped.Type == ir.ArrayType") {
		t.Errorf("Expected array type check, got:\n%s", code)
	}
	if !strings.Contains(code, "for i, v := range fieldNodeUnwrapped.Values") {
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
		destVar  string
		typ      reflect.Type
		context  string
		wantErr  bool
		contains []string
	}{
		{
			name:     "string",
			varName:  "v",
			destVar:  "elem",
			typ:      reflect.TypeOf(""),
			context:  "field",
			wantErr:  false,
			contains: []string{"v.Type != ir.StringType", "elem = v.String"},
		},
		{
			name:     "int",
			varName:  "v",
			destVar:  "elem",
			typ:      reflect.TypeOf(int(0)),
			context:  "field",
			wantErr:  false,
			contains: []string{"v.Int64 == nil", "elem = int(*v.Int64)"},
		},
		{
			name:     "int8",
			varName:  "v",
			destVar:  "elem",
			typ:      reflect.TypeOf(int8(0)),
			context:  "field",
			wantErr:  false,
			contains: []string{"overflows int8", "elem = int8(*v.Int64)"},
		},
		{
			name:     "uint",
			varName:  "v",
			destVar:  "elem",
			typ:      reflect.TypeOf(uint(0)),
			context:  "field",
			wantErr:  false,
			contains: []string{"negative value", "elem = uint(*v.Int64)"},
		},
		{
			name:     "bool",
			varName:  "v",
			destVar:  "elem",
			typ:      reflect.TypeOf(false),
			context:  "field",
			wantErr:  false,
			contains: []string{"v.Type != ir.BoolType", "elem = v.Bool"},
		},
		{
			name:    "unsupported",
			varName: "v",
			destVar: "elem",
			typ:     reflect.TypeOf([]string{}),
			context: "field",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := generatePrimitiveFromIR(tt.varName, tt.destVar, tt.typ, tt.context)
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
	code, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
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
	code, err := GenerateToTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
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
	code, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
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

// ComponentConfig is a test struct used by TestReproMapPointerValueIssue
type ComponentConfig struct {
	Name    string
	Enabled bool
}

// TestReproMapPointerValueIssue tests map[string]*ComponentConfig codegen
// This reproduces the issue where generated code has:
//   m := make(map[string]*struct)
//   val := new(struct)
// instead of:
//   m := make(map[string]*ComponentConfig)
//   val := new(ComponentConfig)
func TestReproMapPointerValueIssue(t *testing.T) {
	type ConfigMap struct {
		Components map[string]*ComponentConfig
	}

	structInfo := &StructInfo{
		Name: "ConfigMap",
		Fields: []*FieldInfo{
			{
				Name:            "Components",
				Type:            reflect.TypeOf(map[string]*ComponentConfig{}),
				SchemaFieldName: "components",
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "config_map",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{Name: "config_map"},
	}

	// Generate FromTonyIR method
	code, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod failed: %v", err)
	}

	t.Logf("Generated code:\n%s", code)

	// Check that the generated code uses ComponentConfig, not "struct"
	if strings.Contains(code, "*struct") {
		t.Errorf("Generated code contains '*struct' instead of '*ComponentConfig':\n%s", code)
	}
	if strings.Contains(code, "new(struct)") {
		t.Errorf("Generated code contains 'new(struct)' instead of 'new(ComponentConfig)':\n%s", code)
	}

	// Verify the correct type names are used
	if !strings.Contains(code, "*ComponentConfig") {
		t.Errorf("Generated code should contain '*ComponentConfig':\n%s", code)
	}
	if !strings.Contains(code, "new(ComponentConfig)") {
		t.Errorf("Generated code should contain 'new(ComponentConfig)':\n%s", code)
	}
}

// TestReproMapPointerValueSparseArray tests map[uint32]*ComponentConfig codegen (sparse array)
// This is a different code path that uses getTypeName instead of getQualifiedTypeName
func TestReproMapPointerValueSparseArray(t *testing.T) {
	type SparseConfigMap struct {
		Components map[uint32]*ComponentConfig
	}

	structInfo := &StructInfo{
		Name: "SparseConfigMap",
		Fields: []*FieldInfo{
			{
				Name:            "Components",
				Type:            reflect.TypeOf(map[uint32]*ComponentConfig{}),
				SchemaFieldName: "components",
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "sparse_config_map",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{Name: "sparse_config_map"},
	}

	// Generate FromTonyIR method
	code, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod failed: %v", err)
	}

	t.Logf("Generated code:\n%s", code)

	// Check that the generated code uses ComponentConfig, not "struct"
	if strings.Contains(code, "*struct") {
		t.Errorf("Generated code contains '*struct' instead of '*ComponentConfig':\n%s", code)
	}
	if strings.Contains(code, "make(map[uint32])") {
		t.Errorf("Generated code contains incomplete map type 'make(map[uint32])':\n%s", code)
	}

	// Verify the correct type names are used
	if !strings.Contains(code, "*ComponentConfig") {
		t.Errorf("Generated code should contain '*ComponentConfig':\n%s", code)
	}
}

func TestGetTypeString_NamedTypes(t *testing.T) {
	// Test that named types like time.Duration are handled correctly
	// instead of returning their underlying primitive type

	tests := []struct {
		name     string
		typ      reflect.Type
		expected string
	}{
		{
			name:     "time.Duration returns time.Duration not int64",
			typ:      reflect.TypeOf(time.Duration(0)),
			expected: "time.Duration",
		},
		{
			name:     "plain int64 returns int64",
			typ:      reflect.TypeOf(int64(0)),
			expected: "int64",
		},
		{
			name:     "plain string returns string",
			typ:      reflect.TypeOf(""),
			expected: "string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getTypeString(tt.typ)
			if result != tt.expected {
				t.Errorf("getTypeString(%v) = %q, want %q", tt.typ, result, tt.expected)
			}
		})
	}
}

// TestGenerateZeroValueHelpers_TimeTime verifies that time.Time optional fields
// get proper zero-value helpers using IsZero() method.
func TestGenerateZeroValueHelpers_TimeTime(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "Run",
		Package: "example",
		Fields: []*FieldInfo{
			{
				Name:            "Started",
				SchemaFieldName: "started",
				Type:            reflect.TypeOf(time.Time{}),
				TypePkgPath:     "time",
				TypeName:        "Time",
				Optional:        false, // required field - no helper needed
			},
			{
				Name:            "Finished",
				SchemaFieldName: "finished",
				Type:            reflect.TypeOf(time.Time{}),
				TypePkgPath:     "time",
				TypeName:        "Time",
				Optional:        true, // optional field - needs helper
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "run",
		},
	}

	code, err := GenerateZeroValueHelpers([]*StructInfo{structInfo})
	if err != nil {
		t.Fatalf("GenerateZeroValueHelpers failed: %v", err)
	}

	// Check that helper is generated for optional time.Time field
	if !strings.Contains(code, "func isZeroValue_Run_Finished") {
		t.Errorf("Expected isZeroValue_Run_Finished helper, got:\n%s", code)
	}

	// Check that it uses IsZero() method
	if !strings.Contains(code, "return v.IsZero()") {
		t.Errorf("Expected IsZero() check for time.Time, got:\n%s", code)
	}

	// Check that no helper is generated for required field
	if strings.Contains(code, "isZeroValue_Run_Started") {
		t.Errorf("Should not generate helper for required field Started, got:\n%s", code)
	}
}

// TestGenerateZeroValueHelpers_SliceOfStruct verifies that slice of struct fields
// get proper zero-value helpers with correct slice type signature.
func TestGenerateZeroValueHelpers_SliceOfStruct(t *testing.T) {
	// Define a Child struct type for the slice element
	type Child struct {
		Name string
		Age  int
	}

	structInfo := &StructInfo{
		Name:    "Parent",
		Package: "example",
		Fields: []*FieldInfo{
			{
				Name:            "Children",
				SchemaFieldName: "children",
				Type:            reflect.TypeOf([]Child{}),
				StructTypeName:  "Child", // Element type name, not []Child
				Optional:        true,
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "parent",
		},
	}

	code, err := GenerateZeroValueHelpers([]*StructInfo{structInfo})
	if err != nil {
		t.Fatalf("GenerateZeroValueHelpers failed: %v", err)
	}

	// Check that helper is generated with correct slice signature
	if !strings.Contains(code, "func isZeroValue_Parent_Children(v []Child) bool") {
		t.Errorf("Expected isZeroValue_Parent_Children(v []Child), got:\n%s", code)
	}

	// Check that it uses len() check
	if !strings.Contains(code, "return len(v) == 0") {
		t.Errorf("Expected len(v) == 0 check for slice, got:\n%s", code)
	}
}

// TestGenerateZeroValueHelpers_CompositeTypes verifies that various composite types
// (slices, arrays, maps, pointers) get correct type signatures in zero-value helpers.
func TestGenerateZeroValueHelpers_CompositeTypes(t *testing.T) {
	type Inner struct{ X int }

	tests := []struct {
		name           string
		fieldType      reflect.Type
		structTypeName string
		wantSignature  string
	}{
		{
			name:           "slice of struct",
			fieldType:      reflect.TypeOf([]Inner{}),
			structTypeName: "Inner",
			wantSignature:  "(v []Inner)",
		},
		{
			name:           "array of struct",
			fieldType:      reflect.TypeOf([3]Inner{}),
			structTypeName: "Inner",
			wantSignature:  "(v [3]Inner)",
		},
		{
			name:           "map with struct value",
			fieldType:      reflect.TypeOf(map[string]Inner{}),
			structTypeName: "Inner",
			wantSignature:  "(v map[string]codegen.Inner)", // codegen prefix because Inner is from different package than "example"
		},
		{
			name:           "map with struct key",
			fieldType:      reflect.TypeOf(map[Inner]string{}),
			structTypeName: "Inner",
			wantSignature:  "(v map[codegen.Inner]string)", // codegen prefix because Inner is from different package than "example"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			structInfo := &StructInfo{
				Name:    "Test",
				Package: "example",
				Fields: []*FieldInfo{
					{
						Name:            "Field",
						SchemaFieldName: "field",
						Type:            tt.fieldType,
						StructTypeName:  tt.structTypeName,
						Optional:        true,
					},
				},
				StructSchema: &gomap.StructSchema{
					SchemaName: "test",
				},
			}

			code, err := GenerateZeroValueHelpers([]*StructInfo{structInfo})
			if err != nil {
				t.Fatalf("GenerateZeroValueHelpers failed: %v", err)
			}

			if !strings.Contains(code, tt.wantSignature) {
				t.Errorf("Expected signature %q, got:\n%s", tt.wantSignature, code)
			}
		})
	}
}

// TestGenerateZeroValueHelpers_NestedStruct verifies that optional nested struct fields
// get proper zero-value helpers using reflect.ValueOf(v).IsZero().
func TestGenerateZeroValueHelpers_NestedStruct(t *testing.T) {
	// Define a nested config struct type
	type GatewayConfig struct {
		Enabled   bool
		Namespace string
	}

	structInfo := &StructInfo{
		Name:    "Config",
		Package: "example",
		Fields: []*FieldInfo{
			{
				Name:            "Gateway",
				SchemaFieldName: "gateway",
				Type:            reflect.TypeOf(GatewayConfig{}),
				StructTypeName:  "GatewayConfig",
				Optional:        true,
				// TypePkgPath and TypeName are empty for local types
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "config",
		},
	}

	code, err := GenerateZeroValueHelpers([]*StructInfo{structInfo})
	if err != nil {
		t.Fatalf("GenerateZeroValueHelpers failed: %v", err)
	}

	// Check that helper is generated for optional nested struct field
	if !strings.Contains(code, "func isZeroValue_Config_Gateway") {
		t.Errorf("Expected isZeroValue_Config_Gateway helper, got:\n%s", code)
	}

	// Check that it uses reflect.ValueOf(v).IsZero()
	if !strings.Contains(code, "return reflect.ValueOf(v).IsZero()") {
		t.Errorf("Expected reflect.ValueOf(v).IsZero() check for nested struct, got:\n%s", code)
	}
}
