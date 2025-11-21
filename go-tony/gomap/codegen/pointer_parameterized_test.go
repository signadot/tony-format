package codegen

import (
	"reflect"
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
)

func TestGoTypeToSchemaNode_PointerParameterized(t *testing.T) {
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
			name:    "pointer to string",
			typ:     reflect.TypeOf((*string)(nil)),
			wantTag: "",
			wantStr: ".[nullable(string)]",
			wantErr: false,
		},
		{
			name:    "pointer to int",
			typ:     reflect.TypeOf((*int)(nil)),
			wantTag: "",
			wantStr: ".[nullable(int)]",
			wantErr: false,
		},
		{
			name:    "pointer to bool",
			typ:     reflect.TypeOf((*bool)(nil)),
			wantTag: "",
			wantStr: ".[nullable(bool)]",
			wantErr: false,
		},
		{
			name:    "pointer to float64",
			typ:     reflect.TypeOf((*float64)(nil)),
			wantTag: "",
			wantStr: ".[nullable(float)]",
			wantErr: false,
		},
		{
			name: "pointer to struct with schema",
			typ:  reflect.TypeOf((*struct{ Name string })(nil)),
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
			wantStr: ".[nullable(person)]",
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
