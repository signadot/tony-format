package codegen

import (
	"reflect"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/schema"
)

func TestGenerateCode_Imports(t *testing.T) {
	tests := []struct {
		name        string
		fields      []*FieldInfo
		wantStrconv bool
		wantUnsafe  bool
	}{
		{
			name: "Simple struct, no special imports",
			fields: []*FieldInfo{
				{
					Name:            "Name",
					SchemaFieldName: "name",
					Type:            reflect.TypeOf(""),
				},
			},
			wantStrconv: false,
			wantUnsafe:  false,
		},
		{
			name: "Struct with map[uint32]string, needs strconv",
			fields: []*FieldInfo{
				{
					Name:            "Data",
					SchemaFieldName: "data",
					Type:            reflect.TypeOf(map[uint32]string{}),
				},
			},
			wantStrconv: true,
			wantUnsafe:  false,
		},
		{
			name: "Struct with map[*int]string, needs unsafe",
			fields: []*FieldInfo{
				{
					Name:            "Data",
					SchemaFieldName: "data",
					Type:            reflect.TypeOf(map[*int]string{}),
				},
			},
			wantStrconv: false,
			wantUnsafe:  true,
		},
		{
			name: "Struct with both, needs both",
			fields: []*FieldInfo{
				{
					Name:            "Data1",
					SchemaFieldName: "data1",
					Type:            reflect.TypeOf(map[uint32]string{}),
				},
				{
					Name:            "Data2",
					SchemaFieldName: "data2",
					Type:            reflect.TypeOf(map[*int]string{}),
				},
			},
			wantStrconv: true,
			wantUnsafe:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			structInfo := &StructInfo{
				Name:    "TestStruct",
				Package: "testpkg",
				Fields:  tt.fields,
				StructSchema: &gomap.StructSchema{
					SchemaName: "test_struct",
				},
			}

			schemas := map[string]*schema.Schema{
				"test_struct": {
					Signature: &schema.Signature{
						Name: "test_struct",
					},
				},
			}

			config := &CodegenConfig{
				Package: &PackageInfo{
					Name: "testpkg",
				},
			}

			code, err := GenerateCode([]*StructInfo{structInfo}, schemas, config)
			if err != nil {
				t.Fatalf("GenerateCode failed: %v", err)
			}

			hasStrconv := strings.Contains(code, `"strconv"`)
			if hasStrconv != tt.wantStrconv {
				t.Errorf("strconv import: got %v, want %v", hasStrconv, tt.wantStrconv)
			}

			hasUnsafe := strings.Contains(code, `"unsafe"`)
			if hasUnsafe != tt.wantUnsafe {
				t.Errorf("unsafe import: got %v, want %v", hasUnsafe, tt.wantUnsafe)
			}
		})
	}
}
