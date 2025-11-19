package codegen

import (
	"reflect"
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
)

func TestTypeToSchemaRef(t *testing.T) {
	tests := []struct {
		name        string
		typ         reflect.Type
		fieldInfo   *FieldInfo
		structMap   map[string]*StructInfo
		currentPkg  string
		wantRef     string
		wantErr     bool
		errContains string
	}{
		{
			name:    "string type",
			typ:     reflect.TypeOf(""),
			wantRef: "string",
			wantErr: false,
		},
		{
			name:    "int type",
			typ:     reflect.TypeOf(int(0)),
			wantRef: "int",
			wantErr: false,
		},
		{
			name:    "int64 type",
			typ:     reflect.TypeOf(int64(0)),
			wantRef: "int",
			wantErr: false,
		},
		{
			name:    "uint32 type",
			typ:     reflect.TypeOf(uint32(0)),
			wantRef: "int",
			wantErr: false,
		},
		{
			name:    "float64 type",
			typ:     reflect.TypeOf(float64(0)),
			wantRef: "float",
			wantErr: false,
		},
		{
			name:    "float32 type",
			typ:     reflect.TypeOf(float32(0)),
			wantRef: "float",
			wantErr: false,
		},
		{
			name:    "bool type",
			typ:     reflect.TypeOf(true),
			wantRef: "bool",
			wantErr: false,
		},
		{
			name: "struct with schema",
			typ:  reflect.TypeOf(struct{ Name string }{}),
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
			wantRef: "person",
			wantErr: false,
		},
		{
			name: "struct without schema",
			typ:  reflect.TypeOf(struct{ Name string }{}),
			fieldInfo: &FieldInfo{
				StructTypeName: "UnknownType",
			},
			structMap:   map[string]*StructInfo{},
			wantErr:     true,
			errContains: "has no schema definition",
		},
		{
			name: "cross-package type",
			typ:  reflect.TypeOf(struct{ Name string }{}),
			fieldInfo: &FieldInfo{
				TypePkgPath: "github.com/example/format",
				TypeName:    "Format",
			},
			currentPkg: "github.com/example/models",
			wantRef:    "format:format",
			wantErr:    false,
		},
		{
			name:        "nil type",
			typ:         nil,
			wantErr:     true,
			errContains: "type is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imports := make(map[string]string)
			gotRef, err := typeToSchemaRef(
				tt.typ,
				tt.fieldInfo,
				tt.structMap,
				tt.currentPkg,
				"",  // currentStructName
				"",  // currentSchemaName
				nil, // loader
				imports,
			)

			if (err != nil) != tt.wantErr {
				t.Errorf("typeToSchemaRef() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				if tt.errContains != "" && err != nil {
					if !contains(err.Error(), tt.errContains) {
						t.Errorf("typeToSchemaRef() error = %v, want error containing %q", err, tt.errContains)
					}
				}
				return
			}

			if gotRef != tt.wantRef {
				t.Errorf("typeToSchemaRef() = %q, want %q", gotRef, tt.wantRef)
			}

			// Check imports for cross-package types
			if tt.fieldInfo != nil && tt.fieldInfo.TypePkgPath != "" && tt.fieldInfo.TypePkgPath != tt.currentPkg {
				expectedPkgName := "format" // Based on the test case
				if imports[tt.fieldInfo.TypePkgPath] != expectedPkgName {
					t.Errorf("typeToSchemaRef() imports[%q] = %q, want %q",
						tt.fieldInfo.TypePkgPath, imports[tt.fieldInfo.TypePkgPath], expectedPkgName)
				}
			}
		})
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
