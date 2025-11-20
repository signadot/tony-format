package gomap

import (
	"reflect"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

func TestToTonyIR_SparseArray(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		checkIR func(*testing.T, *ir.Node)
	}{
		{
			name:  "map[uint32]string",
			input: map[uint32]string{0: "a", 5: "b", 10: "c"},
			checkIR: func(t *testing.T, node *ir.Node) {
				if node.Type != ir.ObjectType {
					t.Fatalf("expected ObjectType, got %v", node.Type)
				}
				if !ir.TagHas(node.Tag, ir.IntKeysTag) {
					t.Error("expected !sparsearray tag")
				}
				// Verify sparse array conversion
				intKeysMap, err := node.ToIntKeysMap()
				if err != nil {
					t.Fatalf("ToIntKeysMap() error = %v", err)
				}
				if intKeysMap[0].String != "a" {
					t.Errorf("intKeysMap[0] = %q, want 'a'", intKeysMap[0].String)
				}
				if intKeysMap[5].String != "b" {
					t.Errorf("intKeysMap[5] = %q, want 'b'", intKeysMap[5].String)
				}
				if intKeysMap[10].String != "c" {
					t.Errorf("intKeysMap[10] = %q, want 'c'", intKeysMap[10].String)
				}
			},
		},
		{
			name:  "map[uint32]int",
			input: map[uint32]int{1: 10, 2: 20},
			checkIR: func(t *testing.T, node *ir.Node) {
				if !ir.TagHas(node.Tag, ir.IntKeysTag) {
					t.Error("expected !sparsearray tag")
				}
				intKeysMap, err := node.ToIntKeysMap()
				if err != nil {
					t.Fatalf("ToIntKeysMap() error = %v", err)
				}
				if intKeysMap[1].Int64 == nil || *intKeysMap[1].Int64 != 10 {
					t.Errorf("intKeysMap[1] = %v, want 10", intKeysMap[1].Int64)
				}
			},
		},
		{
			name:  "map[uint32]interface{}",
			input: map[uint32]interface{}{0: "a", 1: 42, 2: true},
			checkIR: func(t *testing.T, node *ir.Node) {
				if !ir.TagHas(node.Tag, ir.IntKeysTag) {
					t.Error("expected !sparsearray tag")
				}
			},
		},
		{
			name:  "empty sparse array",
			input: map[uint32]string{},
			checkIR: func(t *testing.T, node *ir.Node) {
				if !ir.TagHas(node.Tag, ir.IntKeysTag) {
					t.Error("expected !sparsearray tag")
				}
				intKeysMap, err := node.ToIntKeysMap()
				if err != nil {
					t.Fatalf("ToIntKeysMap() error = %v", err)
				}
				if len(intKeysMap) != 0 {
					t.Errorf("expected empty map, got %d elements", len(intKeysMap))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := ToTonyIR(tt.input)
			if err != nil {
				t.Fatalf("ToIR() error = %v", err)
			}
			if tt.checkIR != nil {
				tt.checkIR(t, node)
			}
		})
	}
}

func TestFromIR_SparseArray(t *testing.T) {
	tests := []struct {
		name    string
		node    *ir.Node
		want    interface{}
		wantErr bool
	}{
		{
			name: "map[uint32]string",
			node: func() *ir.Node {
				m := map[uint32]*ir.Node{
					0:  ir.FromString("a"),
					5:  ir.FromString("b"),
					10: ir.FromString("c"),
				}
				return ir.FromIntKeysMap(m)
			}(),
			want:    map[uint32]string{0: "a", 5: "b", 10: "c"},
			wantErr: false,
		},
		{
			name: "map[uint32]int",
			node: func() *ir.Node {
				m := map[uint32]*ir.Node{
					1: ir.FromInt(10),
					2: ir.FromInt(20),
				}
				return ir.FromIntKeysMap(m)
			}(),
			want:    map[uint32]int{1: 10, 2: 20},
			wantErr: false,
		},
		{
			name: "map[uint32]interface{}",
			node: func() *ir.Node {
				m := map[uint32]*ir.Node{
					0: ir.FromString("a"),
					1: ir.FromInt(42),
					2: ir.FromBool(true),
				}
				return ir.FromIntKeysMap(m)
			}(),
			want: map[uint32]interface{}{
				0: "a",
				1: int64(42),
				2: true,
			},
			wantErr: false,
		},
		{
			name:    "empty sparse array",
			node:    ir.FromIntKeysMap(map[uint32]*ir.Node{}),
			want:    map[uint32]string{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.New(reflect.TypeOf(tt.want))
			err := FromTonyIR(tt.node, val.Interface())
			if (err != nil) != tt.wantErr {
				t.Errorf("FromTonyIR() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				got := val.Elem().Interface()
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("FromTonyIR() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestFromIR_SparseArrayWrongType(t *testing.T) {
	// Create a sparse array node
	node := func() *ir.Node {
		m := map[uint32]*ir.Node{0: ir.FromString("a")}
		return ir.FromIntKeysMap(m)
	}()

	// Try to unmarshal to map[string]string (wrong key type)
	var result map[string]string
	err := FromTonyIR(node, &result)
	if err == nil {
		t.Error("FromTonyIR() expected error for wrong key type, got nil")
	}
}

func TestSparseArrayRoundTrip(t *testing.T) {
	original := map[uint32]interface{}{
		0:  "zero",
		5:  "five",
		10: "ten",
		42: int64(100), // Use int64 since interface{} unmarshaling prefers int64
	}

	// Round trip: Go -> IR -> Go
	node, err := ToTonyIR(original)
	if err != nil {
		t.Fatalf("ToIR() error = %v", err)
	}

	if !ir.TagHas(node.Tag, ir.IntKeysTag) {
		t.Error("expected !sparsearray tag")
	}

	var result map[uint32]interface{}
	err = FromTonyIR(node, &result)
	if err != nil {
		t.Fatalf("FromTonyIR() error = %v", err)
	}

	if !reflect.DeepEqual(result, original) {
		t.Errorf("Round trip failed: got %v, want %v", result, original)
	}
}

func TestToIR_RegularMapStillWorks(t *testing.T) {
	// Regular string-keyed maps should still work as objects
	input := map[string]string{"a": "1", "b": "2"}
	node, err := ToTonyIR(input)
	if err != nil {
		t.Fatalf("ToTonyIR() error = %v", err)
	}

	if node.Type != ir.ObjectType {
		t.Errorf("expected ObjectType, got %v", node.Type)
	}
	if ir.TagHas(node.Tag, ir.IntKeysTag) {
		t.Error("regular map should not have !sparsearray tag")
	}

	irMap := ir.ToMap(node)
	if irMap["a"].String != "1" || irMap["b"].String != "2" {
		t.Errorf("unexpected map values: %v", irMap)
	}
}
