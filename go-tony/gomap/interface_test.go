package gomap

import (
	"reflect"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

func TestFromIR_Interface(t *testing.T) {
	tests := []struct {
		name    string
		node    *ir.Node
		want    interface{}
		wantErr bool
	}{
		{
			name:    "string",
			node:    ir.FromString("hello"),
			want:    "hello",
			wantErr: false,
		},
		{
			name:    "int",
			node:    ir.FromInt(42),
			want:    int64(42),
			wantErr: false,
		},
		{
			name:    "float",
			node:    ir.FromFloat(3.14),
			want:    float64(3.14),
			wantErr: false,
		},
		{
			name:    "bool",
			node:    ir.FromBool(true),
			want:    true,
			wantErr: false,
		},
		{
			name:    "null",
			node:    ir.Null(),
			want:    nil,
			wantErr: false,
		},
		{
			name:    "array",
			node:    ir.FromSlice([]*ir.Node{ir.FromString("a"), ir.FromInt(1)}),
			want:    []interface{}{"a", int64(1)},
			wantErr: false,
		},
		{
			name: "object",
			node: ir.FromMap(map[string]*ir.Node{
				"name": ir.FromString("Alice"),
				"age":  ir.FromInt(30),
			}),
			want: map[string]interface{}{
				"name": "Alice",
				"age":  int64(30),
			},
			wantErr: false,
		},
		{
			name: "nested object",
			node: ir.FromMap(map[string]*ir.Node{
				"person": ir.FromMap(map[string]*ir.Node{
					"name": ir.FromString("Bob"),
				}),
			}),
			want: map[string]interface{}{
				"person": map[string]interface{}{
					"name": "Bob",
				},
			},
			wantErr: false,
		},
		{
			name: "complex nested",
			node: ir.FromMap(map[string]*ir.Node{
				"tags": ir.FromSlice([]*ir.Node{
					ir.FromString("dev"),
					ir.FromString("golang"),
				}),
				"active": ir.FromBool(true),
			}),
			want: map[string]interface{}{
				"tags":   []interface{}{"dev", "golang"},
				"active": true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result interface{}
			err := FromTonyIR(tt.node, &result)
			if (err != nil) != tt.wantErr {
				t.Errorf("FromTonyIR() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if !reflect.DeepEqual(result, tt.want) {
					t.Errorf("FromTonyIR() = %v (%T), want %v (%T)", result, result, tt.want, tt.want)
				}
			}
		})
	}
}

func TestFromIR_InterfaceRoundTrip(t *testing.T) {
	// Test round trip: Go -> IR -> interface{} -> compare
	original := map[string]interface{}{
		"name":   "Charlie",
		"age":    35,
		"active": true,
		"tags":   []interface{}{"manager", "tech"},
		"details": map[string]interface{}{
			"dept":  "eng",
			"level": "senior",
		},
	}

	node, err := ToTonyIR(original)
	if err != nil {
		t.Fatalf("ToTonyIR() error = %v", err)
	}

	var result interface{}
	err = FromTonyIR(node, &result)
	if err != nil {
		t.Fatalf("FromTonyIR() error = %v", err)
	}

	// Compare - note that numbers might be int64 vs float64
	resultMap := result.(map[string]interface{})
	if resultMap["name"] != original["name"] {
		t.Errorf("name = %v, want %v", resultMap["name"], original["name"])
	}
	if resultMap["active"] != original["active"] {
		t.Errorf("active = %v, want %v", resultMap["active"], original["active"])
	}
	// Age might be int64 instead of int
	if age, ok := resultMap["age"].(int64); ok {
		if int(age) != original["age"] {
			t.Errorf("age = %v, want %v", age, original["age"])
		}
	} else {
		t.Errorf("age type = %T, want int64", resultMap["age"])
	}
}

func TestFromIR_InterfaceNumberPreference(t *testing.T) {
	// Test that int64 is preferred over float64 when available
	node := ir.FromInt(42)
	var result interface{}
	err := FromTonyIR(node, &result)
	if err != nil {
		t.Fatalf("FromTonyIR() error = %v", err)
	}

	// Should be int64, not float64
	if _, ok := result.(int64); !ok {
		t.Errorf("result type = %T, want int64", result)
	}
	if result.(int64) != 42 {
		t.Errorf("result = %v, want 42", result)
	}
}
