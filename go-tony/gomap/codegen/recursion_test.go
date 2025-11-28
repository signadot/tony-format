package codegen

import (
	"reflect"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/schema"
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
	if !strings.Contains(toCode, "node, err = v.ToTonyIR(opts...)") {
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
		t.Errorf("Expected element instantiation, got:\n%s", fromCode)
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

	if !strings.Contains(toCode, "node, err = v.ToTonyIR(opts...)") {
		t.Errorf("Expected recursive call on map value, got:\n%s", toCode)
	}

	// Test FromTonyIR generation
	fromCode, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod failed: %v", err)
	}

	if !strings.Contains(fromCode, "val := RecursiveMap{}") {
		t.Errorf("Expected value instantiation, got:\n%s", fromCode)
	}
	if !strings.Contains(fromCode, "val.FromTonyIR(v, opts...)") {
		t.Errorf("Expected recursive call on value, got:\n%s", fromCode)
	}
}

type RecursiveSliceType []RecursiveSliceType

func TestRecursiveSliceType(t *testing.T) {
	// type RecursiveSliceType []RecursiveSliceType
	// This is a named slice type.
	// ToTonyIR should be generated for it.

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

	// Test ToTonyIR generation
	toCode, err := GenerateToTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateToTonyIRMethod failed: %v", err)
	}

	// Should use ir.FromIntKeysMap
	if !strings.Contains(toCode, "ir.FromIntKeysMap") {
		t.Errorf("Expected ir.FromIntKeysMap, got:\n%s", toCode)
	}

	// Test FromTonyIR generation
	fromCode, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod failed: %v", err)
	}

	// Should parse uint32 keys
	if !strings.Contains(fromCode, "strconv.ParseUint") {
		t.Errorf("Expected strconv.ParseUint, got:\n%s", fromCode)
	}
}

type NodeSlice []*ir.Node

func TestNodeSlice(t *testing.T) {
	// type NodeSlice []*ir.Node
	// This should be treated as a slice of *ir.Node
	// ToTonyIR should just pass the nodes through.

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

	// Test ToTonyIR generation
	toCode, err := GenerateToTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateToTonyIRMethod failed: %v", err)
	}

	// Should assign v directly (since v is *ir.Node)
	// slice[i] = v
	if !strings.Contains(toCode, "slice[i] = v") {
		t.Errorf("Expected direct assignment of *ir.Node, got:\n%s", toCode)
	}

	// Test FromTonyIR generation
	fromCode, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod failed: %v", err)
	}

	// Should assign v directly
	// slice[i] = v
	if !strings.Contains(fromCode, "slice[i] = v") {
		t.Errorf("Expected direct assignment of *ir.Node, got:\n%s", fromCode)
	}
}

type ScalarWithMarshalText int

func (s ScalarWithMarshalText) MarshalText() ([]byte, error) {
	return []byte("scalar"), nil
}

func (s *ScalarWithMarshalText) UnmarshalText(text []byte) error {
	*s = 123
	return nil
}

func TestScalarWithMarshalText(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "ScalarWithMarshalText",
		Package: "codegen",
		Type:    reflect.TypeOf(ScalarWithMarshalText(0)),
		StructSchema: &gomap.StructSchema{
			SchemaName: "scalar_with_marshal_text",
		},
		ImplementsTextMarshaler:   true,
		ImplementsTextUnmarshaler: true,
	}

	s := &schema.Schema{
		Signature: &schema.Signature{
			Name: "scalar_with_marshal_text",
		},
	}

	// Test ToTonyIR generation
	toCode, err := GenerateToTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateToTonyIRMethod failed: %v", err)
	}

	// Should use MarshalText
	if !strings.Contains(toCode, "s.MarshalText()") {
		t.Errorf("Expected s.MarshalText(), got:\n%s", toCode)
	}

	// Test FromTonyIR generation
	fromCode, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod failed: %v", err)
	}

	// Should use UnmarshalText
	if !strings.Contains(fromCode, "s.UnmarshalText") {
		t.Errorf("Expected s.UnmarshalText, got:\n%s", fromCode)
	}
}

type CrossPackageSlice []schema.Signature

func TestCrossPackageSlice(t *testing.T) {
	structInfo := &StructInfo{
		Name:    "CrossPackageSlice",
		Package: "codegen",
		Type:    reflect.TypeOf(CrossPackageSlice{}),
		StructSchema: &gomap.StructSchema{
			SchemaName: "cross_package_slice",
		},
		Imports: map[string]string{
			"schema": "github.com/signadot/tony-format/go-tony/schema",
		},
	}

	s := &schema.Schema{
		Signature: &schema.Signature{
			Name: "cross_package_slice",
		},
	}

	// Test ToTonyIR generation
	toCode, err := GenerateToTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateToTonyIRMethod failed: %v", err)
	}

	// Should not need explicit type name for ToTonyIR as it uses v.ToTonyIR() or (&v).ToTonyIR()
	if !strings.Contains(toCode, ".ToTonyIR(opts...)") {
		t.Errorf("Expected .ToTonyIR(opts...), got:\n%s", toCode)
	}

	// Test FromTonyIR generation
	fromCode, err := GenerateFromTonyIRMethod(structInfo, s, "github.com/signadot/tony-format/go-tony/gomap/codegen")
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod failed: %v", err)
	}

	// Should use qualified type name
	// make([]schema.Signature, ...)
	if !strings.Contains(fromCode, "make([]schema.Signature") {
		t.Errorf("Expected make([]schema.Signature, ...), got:\n%s", fromCode)
	}
}
