package codegen

import (
	"reflect"
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
)

func TestGoTypeToSchemaNode_SliceParameterized(t *testing.T) {
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
			name:    "slice of strings",
			typ:     reflect.TypeOf([]string{}),
			wantTag: "",
			wantStr: ".[array(string)]",
			wantErr: false,
		},
		{
			name:    "slice of ints",
			typ:     reflect.TypeOf([]int{}),
			wantTag: "",
			wantStr: ".[array(int)]",
			wantErr: false,
		},
		{
			name:    "slice of bools",
			typ:     reflect.TypeOf([]bool{}),
			wantTag: "",
			wantStr: ".[array(bool)]",
			wantErr: false,
		},
		{
			name: "slice of structs with schema",
			typ:  reflect.TypeOf([]struct{ Name string }{}),
			fieldInfo: &FieldInfo{
				StructTypeName: "Person",
			},
			structMap: map[string]*StructInfo{
				"Person": {
					Package: "github.com/example/test",
					StructSchema: &gomap.StructSchema{
						Mode:       "schemagen",
						SchemaName: "person",
					},
				},
			},
			wantTag: "",
			wantStr: ".[array(person)]",
			wantErr: false,
		},
		{
			name:    "array of strings",
			typ:     reflect.TypeOf([5]string{}),
			wantTag: "",
			wantStr: ".[array(string)]",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imports := make(map[string]string)
			node, err := GoTypeToSchemaNode(
				tt.typ,
				tt.fieldInfo,
				tt.structMap,
				"github.com/example/test",
				"",
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
