package gomap

import (
	"reflect"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/schema"
)

func TestExtractGoType_OrUnionTypes(t *testing.T) {
	s := &schema.Schema{
		Context: schema.DefaultContext(),
		Define: map[string]*ir.Node{
			"string": &ir.Node{
				Type: ir.StringType,
				Tag:  "!type",
			},
			"number": &ir.Node{
				Type: ir.NumberType,
				Tag:  "!type",
			},
		},
	}

	tests := []struct {
		name    string
		def     *ir.Node
		want    reflect.Type
		wantErr bool
	}{
		{
			name: "!or [string, number] - union struct",
			def: &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!or",
				Values: []*ir.Node{
					&ir.Node{Type: ir.StringType, Tag: "!type"},
					&ir.Node{Type: ir.NumberType, Tag: "!type"},
				},
			},
			want: func() reflect.Type {
				// Expected: struct{ String *string; Float *float64 }
				return reflect.StructOf([]reflect.StructField{
					{Name: "String", Type: reflect.PtrTo(reflect.TypeOf(""))},
					{Name: "Float", Type: reflect.PtrTo(reflect.TypeOf(float64(0)))},
				})
			}(),
		},
		{
			name: "!or [null, string, number] - union struct with null",
			def: &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!or",
				Values: []*ir.Node{
					ir.Null(),
					&ir.Node{Type: ir.StringType, Tag: "!type"},
					&ir.Node{Type: ir.NumberType, Tag: "!type"},
				},
			},
			want: func() reflect.Type {
				// Expected: struct{ String *string; Float *float64 }
				return reflect.StructOf([]reflect.StructField{
					{Name: "String", Type: reflect.PtrTo(reflect.TypeOf(""))},
					{Name: "Float", Type: reflect.PtrTo(reflect.TypeOf(float64(0)))},
				})
			}(),
		},
		{
			name: "!or [string] - single type (no union)",
			def: &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!or",
				Values: []*ir.Node{
					&ir.Node{Type: ir.StringType, Tag: "!type"},
				},
			},
			want: reflect.TypeOf(""), // Single type, not a struct
		},
		{
			name: "!or [null, string] - nullable single type",
			def: &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!or",
				Values: []*ir.Node{
					ir.Null(),
					&ir.Node{Type: ir.StringType, Tag: "!type"},
				},
			},
			want: reflect.PtrTo(reflect.TypeOf("")), // *string
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractGoType(tt.def, s, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractGoType() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			// For struct types, check that they have the same structure
			if got.Kind() == reflect.Struct && tt.want.Kind() == reflect.Struct {
				if got.NumField() != tt.want.NumField() {
					t.Errorf("ExtractGoType() struct field count = %d, want %d", got.NumField(), tt.want.NumField())
					return
				}
				for i := 0; i < got.NumField(); i++ {
					gotField := got.Field(i)
					wantField := tt.want.Field(i)
					if gotField.Name != wantField.Name {
						t.Errorf("ExtractGoType() struct field[%d].Name = %q, want %q", i, gotField.Name, wantField.Name)
					}
					if gotField.Type != wantField.Type {
						t.Errorf("ExtractGoType() struct field[%d].Type = %v, want %v", i, gotField.Type, wantField.Type)
					}
				}
			} else {
				if got != tt.want {
					t.Errorf("ExtractGoType() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
