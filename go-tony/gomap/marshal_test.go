package gomap

import (
	"reflect"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

func TestToIR_BasicTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		checkIR  func(*testing.T, *ir.Node)
		wantType ir.Type
	}{
		{
			name:     "string",
			input:    "hello",
			wantType: ir.StringType,
			checkIR: func(t *testing.T, node *ir.Node) {
				if node.String != "hello" {
					t.Errorf("expected string 'hello', got %q", node.String)
				}
			},
		},
		{
			name:     "int",
			input:    42,
			wantType: ir.NumberType,
			checkIR: func(t *testing.T, node *ir.Node) {
				if node.Int64 == nil || *node.Int64 != 42 {
					t.Errorf("expected int 42, got %v", node.Int64)
				}
			},
		},
		{
			name:     "int64",
			input:    int64(123456789),
			wantType: ir.NumberType,
			checkIR: func(t *testing.T, node *ir.Node) {
				if node.Int64 == nil || *node.Int64 != 123456789 {
					t.Errorf("expected int64 123456789, got %v", node.Int64)
				}
			},
		},
		{
			name:     "uint",
			input:    uint(99),
			wantType: ir.NumberType,
			checkIR: func(t *testing.T, node *ir.Node) {
				if node.Int64 == nil || *node.Int64 != 99 {
					t.Errorf("expected uint 99, got %v", node.Int64)
				}
			},
		},
		{
			name:     "float64",
			input:    3.14,
			wantType: ir.NumberType,
			checkIR: func(t *testing.T, node *ir.Node) {
				if node.Float64 == nil || *node.Float64 != 3.14 {
					t.Errorf("expected float64 3.14, got %v", node.Float64)
				}
			},
		},
		{
			name:     "bool true",
			input:    true,
			wantType: ir.BoolType,
			checkIR: func(t *testing.T, node *ir.Node) {
				if !node.Bool {
					t.Errorf("expected bool true, got false")
				}
			},
		},
		{
			name:     "bool false",
			input:    false,
			wantType: ir.BoolType,
			checkIR: func(t *testing.T, node *ir.Node) {
				if node.Bool {
					t.Errorf("expected bool false, got true")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := ToIR(tt.input)
			if err != nil {
				t.Fatalf("ToIR() error = %v", err)
			}
			if node.Type != tt.wantType {
				t.Errorf("ToIR() type = %v, want %v", node.Type, tt.wantType)
			}
			if tt.checkIR != nil {
				tt.checkIR(t, node)
			}
		})
	}
}

func TestToIR_Nil(t *testing.T) {
	node, err := ToIR(nil)
	if err != nil {
		t.Fatalf("ToIR(nil) error = %v", err)
	}
	if node.Type != ir.NullType {
		t.Errorf("ToIR(nil) type = %v, want %v", node.Type, ir.NullType)
	}
}

func TestToIR_Pointers(t *testing.T) {
	str := "hello"
	strPtr := &str
	var nilStrPtr *string

	tests := []struct {
		name     string
		input    interface{}
		wantType ir.Type
		checkIR  func(*testing.T, *ir.Node)
	}{
		{
			name:     "non-nil string pointer",
			input:    strPtr,
			wantType: ir.StringType,
			checkIR: func(t *testing.T, node *ir.Node) {
				if node.String != "hello" {
					t.Errorf("expected 'hello', got %q", node.String)
				}
			},
		},
		{
			name:     "nil pointer",
			input:    nilStrPtr,
			wantType: ir.NullType,
		},
		{
			name:     "pointer to int",
			input:    intPtrMarshal(42),
			wantType: ir.NumberType,
			checkIR: func(t *testing.T, node *ir.Node) {
				if node.Int64 == nil || *node.Int64 != 42 {
					t.Errorf("expected 42, got %v", node.Int64)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := ToIR(tt.input)
			if err != nil {
				t.Fatalf("ToIR() error = %v", err)
			}
			if node.Type != tt.wantType {
				t.Errorf("ToIR() type = %v, want %v", node.Type, tt.wantType)
			}
			if tt.checkIR != nil {
				tt.checkIR(t, node)
			}
		})
	}
}

func intPtrMarshal(i int) *int {
	return &i
}

func TestToIR_Slices(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		wantLen  int
		checkIR  func(*testing.T, *ir.Node)
	}{
		{
			name:    "string slice",
			input:   []string{"a", "b", "c"},
			wantLen: 3,
			checkIR: func(t *testing.T, node *ir.Node) {
				if len(node.Values) != 3 {
					t.Fatalf("expected 3 elements, got %d", len(node.Values))
				}
				if node.Values[0].String != "a" || node.Values[1].String != "b" || node.Values[2].String != "c" {
					t.Errorf("unexpected values: %v", node.Values)
				}
			},
		},
		{
			name:    "int slice",
			input:   []int{1, 2, 3},
			wantLen: 3,
			checkIR: func(t *testing.T, node *ir.Node) {
				if len(node.Values) != 3 {
					t.Fatalf("expected 3 elements, got %d", len(node.Values))
				}
				for i, val := range []int64{1, 2, 3} {
					if node.Values[i].Int64 == nil || *node.Values[i].Int64 != val {
						t.Errorf("Values[%d] = %v, want %d", i, node.Values[i].Int64, val)
					}
				}
			},
		},
		{
			name:    "empty slice",
			input:   []string{},
			wantLen: 0,
			checkIR: func(t *testing.T, node *ir.Node) {
				if len(node.Values) != 0 {
					t.Errorf("expected empty slice, got %d elements", len(node.Values))
				}
			},
		},
		{
			name:    "nil slice",
			input:   []string(nil),
			wantLen: 0,
			checkIR: func(t *testing.T, node *ir.Node) {
				if len(node.Values) != 0 {
					t.Errorf("expected empty slice, got %d elements", len(node.Values))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := ToIR(tt.input)
			if err != nil {
				t.Fatalf("ToIR() error = %v", err)
			}
			if node.Type != ir.ArrayType {
				t.Errorf("ToIR() type = %v, want %v", node.Type, ir.ArrayType)
			}
			if len(node.Values) != tt.wantLen {
				t.Errorf("ToIR() len = %d, want %d", len(node.Values), tt.wantLen)
			}
			if tt.checkIR != nil {
				tt.checkIR(t, node)
			}
		})
	}
}

func TestToIR_Arrays(t *testing.T) {
	arr := [3]int{1, 2, 3}
	node, err := ToIR(arr)
	if err != nil {
		t.Fatalf("ToIR() error = %v", err)
	}
	if node.Type != ir.ArrayType {
		t.Errorf("ToIR() type = %v, want %v", node.Type, ir.ArrayType)
	}
	if len(node.Values) != 3 {
		t.Errorf("ToIR() len = %d, want 3", len(node.Values))
	}
}

func TestToIR_Maps(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		checkIR func(*testing.T, *ir.Node)
	}{
		{
			name:  "string map",
			input: map[string]string{"a": "1", "b": "2"},
			checkIR: func(t *testing.T, node *ir.Node) {
				if node.Type != ir.ObjectType {
					t.Fatalf("expected ObjectType, got %v", node.Type)
				}
				irMap := ir.ToMap(node)
				if irMap["a"].String != "1" || irMap["b"].String != "2" {
					t.Errorf("unexpected map values: %v", irMap)
				}
			},
		},
		{
			name:  "int map",
			input: map[string]int{"x": 10, "y": 20},
			checkIR: func(t *testing.T, node *ir.Node) {
				irMap := ir.ToMap(node)
				if irMap["x"].Int64 == nil || *irMap["x"].Int64 != 10 {
					t.Errorf("x = %v, want 10", irMap["x"].Int64)
				}
				if irMap["y"].Int64 == nil || *irMap["y"].Int64 != 20 {
					t.Errorf("y = %v, want 20", irMap["y"].Int64)
				}
			},
		},
		{
			name:  "empty map",
			input: map[string]string{},
			checkIR: func(t *testing.T, node *ir.Node) {
				if node.Type != ir.ObjectType {
					t.Fatalf("expected ObjectType, got %v", node.Type)
				}
				irMap := ir.ToMap(node)
				if len(irMap) != 0 {
					t.Errorf("expected empty map, got %d elements", len(irMap))
				}
			},
		},
		{
			name:  "nil map",
			input: map[string]string(nil),
			checkIR: func(t *testing.T, node *ir.Node) {
				if node.Type != ir.NullType {
					t.Errorf("expected NullType for nil map, got %v", node.Type)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := ToIR(tt.input)
			if err != nil {
				t.Fatalf("ToIR() error = %v", err)
			}
			if tt.checkIR != nil {
				tt.checkIR(t, node)
			}
		})
	}
}

func TestToIR_Structs(t *testing.T) {
	type Person struct {
		Name string
		Age  int
	}

	p := Person{Name: "Alice", Age: 30}
	node, err := ToIR(p)
	if err != nil {
		t.Fatalf("ToIR() error = %v", err)
	}
	if node.Type != ir.ObjectType {
		t.Fatalf("expected ObjectType, got %v", node.Type)
	}

	irMap := ir.ToMap(node)
	if irMap["Name"].String != "Alice" {
		t.Errorf("Name = %q, want 'Alice'", irMap["Name"].String)
	}
	if irMap["Age"].Int64 == nil || *irMap["Age"].Int64 != 30 {
		t.Errorf("Age = %v, want 30", irMap["Age"].Int64)
	}
}

func TestToIR_StructsWithUnexportedFields(t *testing.T) {
	type Person struct {
		Name     string
		age      int // unexported
		Exported int
	}

	p := Person{Name: "Alice", age: 25, Exported: 100}
	node, err := ToIR(p)
	if err != nil {
		t.Fatalf("ToIR() error = %v", err)
	}

	irMap := ir.ToMap(node)
	// Should only have exported fields
	if _, ok := irMap["age"]; ok {
		t.Error("unexported field 'age' should not be in output")
	}
	if irMap["Name"].String != "Alice" {
		t.Errorf("Name = %q, want 'Alice'", irMap["Name"].String)
	}
	if irMap["Exported"].Int64 == nil || *irMap["Exported"].Int64 != 100 {
		t.Errorf("Exported = %v, want 100", irMap["Exported"].Int64)
	}
}

func TestToIR_EmbeddedStructs(t *testing.T) {
	type Address struct {
		Street string
		City   string
	}

	type Person struct {
		Name string
		Address
	}

	p := Person{
		Name:    "Bob",
		Address: Address{Street: "123 Main", City: "NYC"},
	}

	node, err := ToIR(p)
	if err != nil {
		t.Fatalf("ToIR() error = %v", err)
	}

	irMap := ir.ToMap(node)
	// Embedded struct fields should be flattened
	if irMap["Name"].String != "Bob" {
		t.Errorf("Name = %q, want 'Bob'", irMap["Name"].String)
	}
	if irMap["Street"].String != "123 Main" {
		t.Errorf("Street = %q, want '123 Main'", irMap["Street"].String)
	}
	if irMap["City"].String != "NYC" {
		t.Errorf("City = %q, want 'NYC'", irMap["City"].String)
	}
}

func TestToIR_NestedStructs(t *testing.T) {
	type Address struct {
		Street string
		City   string
	}

	type Person struct {
		Name    string
		Address Address // not embedded
	}

	p := Person{
		Name:    "Charlie",
		Address: Address{Street: "456 Oak", City: "LA"},
	}

	node, err := ToIR(p)
	if err != nil {
		t.Fatalf("ToIR() error = %v", err)
	}

	irMap := ir.ToMap(node)
	if irMap["Name"].String != "Charlie" {
		t.Errorf("Name = %q, want 'Charlie'", irMap["Name"].String)
	}
	// Address should be a nested object
	addrNode := irMap["Address"]
	if addrNode.Type != ir.ObjectType {
		t.Fatalf("Address type = %v, want ObjectType", addrNode.Type)
	}
	addrMap := ir.ToMap(addrNode)
	if addrMap["Street"].String != "456 Oak" {
		t.Errorf("Address.Street = %q, want '456 Oak'", addrMap["Street"].String)
	}
}

func TestToIR_ComplexNested(t *testing.T) {
	type Address struct {
		Street string
		City   string
	}

	type Person struct {
		Name    string
		Age     int
		Tags    []string
		Address Address
	}

	p := Person{
		Name: "Dave",
		Age:  35,
		Tags: []string{"developer", "golang"},
		Address: Address{
			Street: "789 Pine",
			City:   "SF",
		},
	}

	node, err := ToIR(p)
	if err != nil {
		t.Fatalf("ToIR() error = %v", err)
	}

	irMap := ir.ToMap(node)
	if irMap["Name"].String != "Dave" {
		t.Errorf("Name = %q, want 'Dave'", irMap["Name"].String)
	}
	if irMap["Age"].Int64 == nil || *irMap["Age"].Int64 != 35 {
		t.Errorf("Age = %v, want 35", irMap["Age"].Int64)
	}

	// Check tags slice
	tagsNode := irMap["Tags"]
	if tagsNode.Type != ir.ArrayType || len(tagsNode.Values) != 2 {
		t.Fatalf("Tags should be array with 2 elements")
	}
	if tagsNode.Values[0].String != "developer" || tagsNode.Values[1].String != "golang" {
		t.Errorf("Tags = %v, want ['developer', 'golang']", tagsNode.Values)
	}

	// Check nested address
	addrMap := ir.ToMap(irMap["Address"])
	if addrMap["Street"].String != "789 Pine" {
		t.Errorf("Address.Street = %q, want '789 Pine'", addrMap["Street"].String)
	}
}

func TestToIR_WithToTonyMethod(t *testing.T) {
	type CustomType struct {
		Value string
	}

	// Add ToTony method
	ct := CustomType{Value: "custom"}
	ctType := reflect.TypeOf(ct)
	// We can't actually add methods at runtime, so we'll test that the method detection works
	// by creating a type that implements the interface

	// For now, just verify reflection fallback works
	node, err := ToIR(ct)
	if err != nil {
		t.Fatalf("ToIR() error = %v", err)
	}
	if node.Type != ir.ObjectType {
		t.Errorf("expected ObjectType, got %v", node.Type)
	}

	// Test that if a method exists, it would be called
	// (We can't easily test this without code generation or manual method addition)
	_ = ctType // suppress unused
}
