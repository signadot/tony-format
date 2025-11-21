package codegen

import (
	"reflect"
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
)

func TestGoTypeToSchemaNode_MapParameterized(t *testing.T) {
	tests := []struct {
		name      string
		typ       reflect.Type
		fieldInfo *FieldInfo
		structMap map[string]*StructInfo
		wantTag   string
		wantStr   string
		wantErr   bool
	}{
		{
			name:    "map[string]string",
			typ:     reflect.TypeOf(map[string]string{}),
			wantTag: "",
			wantStr: ".[object(string)]",
			wantErr: false,
		},
		{
			name:    "map[string]int",
			typ:     reflect.TypeOf(map[string]int{}),
			wantTag: "",
			wantStr: ".[object(int)]",
			wantErr: false,
		},
		{
			name:    "map[string]bool",
			typ:     reflect.TypeOf(map[string]bool{}),
			wantTag: "",
			wantStr: ".[object(bool)]",
			wantErr: false,
		},
		{
			name: "map[string]struct with schema",
			typ:  reflect.TypeOf(map[string]struct{ Name string }{}),
			fieldInfo: &FieldInfo{
				StructTypeName: "Person",
			},
			structMap: map[string]*StructInfo{
				"Person": {
					StructSchema: &gomap.StructSchema{
						SchemaName: "person",
					},
				},
			},
			wantTag: "",
			wantStr: ".[object(person)]",
			wantErr: false,
		},
		{
			name:    "map[uint32]string (sparse array)",
			typ:     reflect.TypeOf(map[uint32]string{}),
			wantTag: "",
			wantStr: ".[sparsearray(string)]",
			wantErr: false,
		},
		{
			name:    "map[uint32]int (sparse array)",
			typ:     reflect.TypeOf(map[uint32]int{}),
			wantTag: "",
			wantStr: ".[sparsearray(int)]",
			wantErr: false,
		},
		{
			name: "map[uint32]struct with schema (sparse array)",
			typ:  reflect.TypeOf(map[uint32]struct{ Name string }{}),
			fieldInfo: &FieldInfo{
				StructTypeName: "Person",
			},
			structMap: map[string]*StructInfo{
				"Person": {
					StructSchema: &gomap.StructSchema{
						SchemaName: "person",
					},
				},
			},
			wantTag: "",
			wantStr: ".[sparsearray(person)]",
			wantErr: false,
		},
		{
			name: "map[uint32]CurrentStruct (self-reference)",
			typ:  reflect.TypeOf(map[uint32]struct{ Name string }{}),
			fieldInfo: &FieldInfo{
				StructTypeName: "Node",
			},
			structMap: map[string]*StructInfo{
				"Node": {
					StructSchema: &gomap.StructSchema{
						SchemaName: "node",
					},
				},
			},
			wantTag: "",
			wantStr: ".[sparsearray]",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imports := make(map[string]string)
			currentStructName := ""
			if tt.name == "map[uint32]CurrentStruct (self-reference)" {
				currentStructName = "Node"
			}

			node, err := GoTypeToSchemaNode(
				tt.typ,
				tt.fieldInfo,
				tt.structMap,
				"github.com/example/test",
				currentStructName,
				"",
				nil,
				imports,
			)

			if (err != nil) != tt.wantErr {
				t.Errorf("GoTypeToSchemaNode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if node == nil {
				t.Fatal("GoTypeToSchemaNode() returned nil node")
			}

			// Check tag
			if node.Tag != tt.wantTag {
				t.Errorf("GoTypeToSchemaNode() node.Tag = %q, want %q", node.Tag, tt.wantTag)
			}

			// Check string value (for parameterized types, it's a string node)
			if node.String != tt.wantStr {
				t.Errorf("GoTypeToSchemaNode() node.String = %q, want %q", node.String, tt.wantStr)
			}
		})
	}
}
