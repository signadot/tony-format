package codegen

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/schema"
	"github.com/signadot/tony-format/go-tony/stream"
)

type RecursiveSlice struct {
	Children []RecursiveSlice
}

func TestRecursiveSlice(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "RecursiveSlice",
		Package: "codegen",
		Fields: []*FieldInfo{
			{
				Name:            "Children",
				SchemaFieldName: "children",
				Type:            reflect.TypeOf([]RecursiveSlice{}),
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "recursive_slice",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{
			Name: "recursive_slice",
		},
	}

	// Test ToTonyIR generation
	toCode, err := GenerateToTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateToTonyIRMethod failed: %v", err)
	}

	// Check if it calls ToTonyIR on the element (which is a struct value)
	// It should look like: node, err = v.ToTonyIR(opts...)
	// Wait, v is a value, so it should be (&v).ToTonyIR(opts...)
	if !strings.Contains(toCode, "v.ToTonyIR(opts...)") {
		t.Errorf("Expected recursive call on slice element, got:\n%s", toCode)
	}

	// Test FromTonyIR generation
	fromCode, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod failed: %v", err)
	}

	// Check if it handles struct value elements correctly
	// It should look like:
	// elem := RecursiveSlice{}
	// if err := elem.FromTonyIR(v, opts...); err != nil { ... }
	if !strings.Contains(fromCode, "elem := RecursiveSlice{}") {
		t.Errorf("Expected element initialization, got:\n%s", fromCode)
	}
	if !strings.Contains(fromCode, "elem.FromTonyIR(v, opts...)") {
		t.Errorf("Expected recursive call on element, got:\n%s", fromCode)
	}
}

type RecursiveMap struct {
	Children map[string]RecursiveMap
}

func TestRecursiveMap(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "RecursiveMap",
		Package: "codegen",
		Fields: []*FieldInfo{
			{
				Name:            "Children",
				SchemaFieldName: "children",
				Type:            reflect.TypeOf(map[string]RecursiveMap{}),
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "recursive_map",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{
			Name: "recursive_map",
		},
	}

	// Test ToTonyIR generation
	toCode, err := GenerateToTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateToTonyIRMethod failed: %v", err)
	}

	// Check if it calls ToTonyIR on the value
	if !strings.Contains(toCode, "v.ToTonyIR(opts...)") {
		t.Errorf("Expected recursive call on map value, got:\n%s", toCode)
	}

	// Test FromTonyIR generation
	fromCode, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod failed: %v", err)
	}

	// Check if it handles struct value elements correctly
	if !strings.Contains(fromCode, "val := RecursiveMap{}") {
		t.Errorf("Expected value initialization, got:\n%s", fromCode)
	}
	if !strings.Contains(fromCode, "val.FromTonyIR(v, opts...)") {
		t.Errorf("Expected recursive call on value, got:\n%s", fromCode)
	}
}

type RecursiveSliceType []RecursiveSliceType

func TestRecursiveSliceType(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "RecursiveSliceType",
		Package: "codegen",
		Type:    reflect.TypeOf(RecursiveSliceType{}),
		StructSchema: &gomap.StructSchema{
			SchemaName: "recursive_slice_type",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{
			Name: "recursive_slice_type",
		},
	}

	// Test ToTonyIR generation
	toCode, err := GenerateToTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateToTonyIRMethod failed: %v", err)
	}

	// For a named slice type, ToTonyIR should iterate over itself
	// func (s *RecursiveSliceType) ToTonyIR(...)
	// It should cast *s to []RecursiveSliceType
	// And iterate.
	// The element type is RecursiveSliceType.
	// So it should call v.ToTonyIR() recursively.

	if !strings.Contains(toCode, "range *s") {
		t.Errorf("Expected iteration over *s, got:\n%s", toCode)
	}
	if !strings.Contains(toCode, "(&v).ToTonyIR(opts...)") {
		t.Errorf("Expected recursive call on element, got:\n%s", toCode)
	}

	// Test FromTonyIR generation
	fromCode, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod failed: %v", err)
	}

	// It should create a slice of RecursiveSliceType
	// slice := make([]RecursiveSliceType, len(...))
	// And call FromTonyIR on elements.
	if !strings.Contains(fromCode, "make([]RecursiveSliceType") {
		t.Errorf("Expected slice creation, got:\n%s", fromCode)
	}
}

type SparseArrayType map[uint32]string

func TestSparseArrayType(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "SparseArrayType",
		Package: "codegen",
		Type:    reflect.TypeOf(SparseArrayType{}),
		StructSchema: &gomap.StructSchema{
			SchemaName: "sparse_array_type",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{
			Name: "sparse_array_type",
		},
	}

	toCode, err := GenerateToTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateToTonyIRMethod failed: %v", err)
	}
	if !strings.Contains(toCode, "ir.FromIntKeysMap") {
		t.Errorf("Expected ir.FromIntKeysMap, got:\n%s", toCode)
	}

	fromCode, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod failed: %v", err)
	}
	if !strings.Contains(fromCode, "strconv.ParseUint") {
		t.Errorf("Expected strconv.ParseUint, got:\n%s", fromCode)
	}
}

type NodeSlice []*ir.Node

func TestNodeSlice(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "NodeSlice",
		Package: "codegen",
		Type:    reflect.TypeOf(NodeSlice{}),
		StructSchema: &gomap.StructSchema{
			SchemaName: "node_slice",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{
			Name: "node_slice",
		},
	}

	toCode, err := GenerateToTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateToTonyIRMethod failed: %v", err)
	}
	if !strings.Contains(toCode, "slice[i] = v") {
		t.Errorf("Expected direct assignment of *ir.Node, got:\n%s", toCode)
	}

	fromCode, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod failed: %v", err)
	}
	if !strings.Contains(fromCode, "slice[i] = v") {
		t.Errorf("Expected direct assignment of *ir.Node, got:\n%s", fromCode)
	}
}

type ScalarWithMarshalText struct {
	Value string
}

func (s *ScalarWithMarshalText) MarshalText() ([]byte, error) {
	return []byte(s.Value), nil
}

func (s *ScalarWithMarshalText) UnmarshalText(text []byte) error {
	s.Value = string(text)
	return nil
}

func TestScalarWithMarshalText(t *testing.T) {
	structInfo := &StructInfo{
		Name:                      "ScalarWithMarshalText",
		Package:                   "codegen",
		Type:                      reflect.TypeOf(ScalarWithMarshalText{}),
		StructSchema:              &gomap.StructSchema{SchemaName: "scalar"},
		ImplementsTextMarshaler:   true,
		ImplementsTextUnmarshaler: true,
	}

	s := &schema.Schema{Signature: &schema.Signature{Name: "scalar"}}

	toCode, err := GenerateToTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateToTonyIRMethod failed: %v", err)
	}
	if !strings.Contains(toCode, "s.MarshalText()") {
		t.Errorf("Expected s.MarshalText(), got:\n%s", toCode)
	}

	fromCode, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod failed: %v", err)
	}
	if !strings.Contains(fromCode, "s.UnmarshalText") {
		t.Errorf("Expected s.UnmarshalText, got:\n%s", fromCode)
	}
}

func TestTimeTimeField(t *testing.T) {
	// Test that *time.Time fields are correctly detected as implementing TextMarshaler/TextUnmarshaler
	structInfo := &StructInfo{
		Name:    "TestStruct",
		Package: "codegen",
		Fields: []*FieldInfo{
			{
				Name:            "When",
				SchemaFieldName: "when",
				Type:            reflect.TypeOf((*time.Time)(nil)).Elem(),
				TypePkgPath:     "time",
				TypeName:        "Time",
				StructTypeName:  "time.Time",
				ImplementsTextMarshaler:   true,
				ImplementsTextUnmarshaler: true,
			},
		},
		StructSchema: &gomap.StructSchema{SchemaName: "test"},
	}

	s := &schema.Schema{Signature: &schema.Signature{Name: "test"}}

	toCode, err := GenerateToTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateToTonyIRMethod failed: %v", err)
	}
	// Should use MarshalText, not ToTonyIR
	if !strings.Contains(toCode, "s.When.MarshalText()") {
		t.Errorf("Expected s.When.MarshalText(), got:\n%s", toCode)
	}
	if strings.Contains(toCode, "s.When.ToTonyIR") {
		t.Errorf("Should not use ToTonyIR for time.Time, got:\n%s", toCode)
	}

	fromCode, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod failed: %v", err)
	}
	// Should use UnmarshalText, not FromTonyIR
	if !strings.Contains(fromCode, "s.When.UnmarshalText") {
		t.Errorf("Expected s.When.UnmarshalText, got:\n%s", fromCode)
	}
	if strings.Contains(fromCode, "s.When.FromTonyIR") {
		t.Errorf("Should not use FromTonyIR for time.Time, got:\n%s", fromCode)
	}
}

// CrossPackageSlice uses stream.Event which has ToTonyIR(opts...) and FromTonyIR(opts...)
type CrossPackageSlice []stream.Event

func TestCrossPackageSlice(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "CrossPackageSlice",
		Package: "codegen",
		Type:    reflect.TypeOf(CrossPackageSlice{}),
		StructSchema: &gomap.StructSchema{
			SchemaName: "cross_package_slice",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{
			Name: "cross_package_slice",
		},
	}

	toCode, err := GenerateToTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateToTonyIRMethod failed: %v", err)
	}
	// It should call ToTonyIR with opts on stream.Event elements (which has ToTonyIR(opts...))
	if !strings.Contains(toCode, "(&v).ToTonyIR(opts...)") {
		t.Errorf("Expected (&v).ToTonyIR(opts...), got:\n%s", toCode)
	}

	fromCode, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod failed: %v", err)
	}
	// It should create a slice of stream.Event
	if !strings.Contains(fromCode, "make([]stream.Event") {
		t.Errorf("Expected make([]stream.Event, ...), got:\n%s", fromCode)
	}
}

type PointerSlice struct {
	Items []*RecursiveSlice
}

func TestPointerSlice(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "PointerSlice",
		Package: "codegen",
		Fields: []*FieldInfo{
			{
				Name:            "Items",
				SchemaFieldName: "items",
				Type:            reflect.TypeOf([]*RecursiveSlice{}),
			},
		},
		StructSchema: &gomap.StructSchema{
			SchemaName: "pointer_slice",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{
			Name: "pointer_slice",
		},
	}

	// Test FromTonyIR generation
	fromCode, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod failed: %v", err)
	}

	// Check that slice creation uses the correct type: []*RecursiveSlice
	if !strings.Contains(fromCode, "make([]*RecursiveSlice") {
		t.Errorf("Expected slice creation with pointer type, got:\n%s", fromCode)
	}
}

// TestExternalPackageTextUnmarshaler tests that types from external packages
// implementing TextUnmarshaler are correctly detected during type resolution.
// This specifically tests the ResolveFieldTypes function with cross-package types.
func TestExternalPackageTextUnmarshaler(t *testing.T) {
	// Parse the pkg5 testdata which has a struct with format.Format field
	pkgDir := "testdata/pkg5"
	filePath := pkgDir + "/types.go"

	file, _, err := ParseFile(filePath)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	structs, err := ExtractTypes(file, filePath)
	if err != nil {
		t.Fatalf("ExtractTypes failed: %v", err)
	}

	if len(structs) == 0 {
		t.Fatal("Expected at least one struct")
	}

	// Set package name
	for _, s := range structs {
		s.Package = "pkg5"
	}

	// Resolve field types - this is what we're testing
	if err := ResolveFieldTypes(structs, pkgDir, "pkg5"); err != nil {
		t.Fatalf("ResolveFieldTypes failed: %v", err)
	}

	// Find the ExternalTextUnmarshaler struct
	var extStruct *StructInfo
	for _, s := range structs {
		if s.Name == "ExternalTextUnmarshaler" {
			extStruct = s
			break
		}
	}
	if extStruct == nil {
		t.Fatal("ExternalTextUnmarshaler struct not found")
	}

	// Find the Format field
	var formatField *FieldInfo
	for _, f := range extStruct.Fields {
		if f.Name == "Format" {
			formatField = f
			break
		}
	}
	if formatField == nil {
		t.Fatal("Format field not found")
	}

	// Verify that TextUnmarshaler was detected for the external type
	if !formatField.ImplementsTextUnmarshaler {
		t.Errorf("Expected format.Format to implement TextUnmarshaler, but ImplementsTextUnmarshaler is false")
	}
	if !formatField.ImplementsTextMarshaler {
		t.Errorf("Expected format.Format to implement TextMarshaler, but ImplementsTextMarshaler is false")
	}

	// Also verify the type info is correctly set
	if formatField.TypePkgPath != "github.com/signadot/tony-format/go-tony/format" {
		t.Errorf("Expected TypePkgPath 'github.com/signadot/tony-format/go-tony/format', got %q", formatField.TypePkgPath)
	}
	if formatField.TypeName != "Format" {
		t.Errorf("Expected TypeName 'Format', got %q", formatField.TypeName)
	}
}
