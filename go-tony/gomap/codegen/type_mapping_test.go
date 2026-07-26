package codegen

import (
	"reflect"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/gomap"
)

func TestTypeToSchemaRef(t *testing.T) {
	tests := []struct {
		name       string
		typ        reflect.Type
		fieldInfo  *FieldInfo
		structMap  map[string]*StructInfo
		currentPkg string
		loader     *PackageLoader
		wantRef    string
		// wantImportPkg is the local name the cross-package import must be
		// registered under; empty means no import should be recorded.
		wantImportPkg string
		wantErr       bool
		errContains   string
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
					Package: "", // same package
					StructSchema: &gomap.StructSchema{
						Mode:       "schemagen",
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
			// A cross-package type is referenced only when the target declares
			// a schema. format.Format does, in format/format.tony.
			name: "cross-package type with a schema",
			typ:  reflect.TypeOf(struct{ Name string }{}),
			fieldInfo: &FieldInfo{
				TypePkgPath: "github.com/signadot/tony-format/go-tony/format",
				TypeName:    "Format",
			},
			currentPkg:    "github.com/signadot/tony-format/go-tony/dirbuild",
			loader:        NewPackageLoader(),
			wantRef:       "format:format",
			wantImportPkg: "format",
			wantErr:       false,
		},
		{
			// time.Duration declares nothing, so there is no reference to make:
			// describe what the encoder emits instead of naming a schema that
			// cannot be looked up.
			name: "cross-package type without a schema",
			typ:  reflect.TypeOf(time.Duration(0)),
			fieldInfo: &FieldInfo{
				TypePkgPath: "time",
				TypeName:    "Duration",
			},
			currentPkg: "github.com/signadot/tony-format/go-tony/system/logd/server",
			loader:     NewPackageLoader(),
			wantRef:    "int",
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
				"", // currentStructName
				"", // currentSchemaName
				tt.loader,
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

			// A reference must bring its import with it; a type that resolved
			// to a plain kind must not leave a dangling one behind.
			if tt.fieldInfo != nil && tt.fieldInfo.TypePkgPath != "" {
				if got := imports[tt.fieldInfo.TypePkgPath]; got != tt.wantImportPkg {
					t.Errorf("typeToSchemaRef() imports[%q] = %q, want %q",
						tt.fieldInfo.TypePkgPath, got, tt.wantImportPkg)
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
