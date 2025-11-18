package gomap

import (
	"reflect"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/schema"
)

func TestExtractGoType_ParameterizedReferences(t *testing.T) {
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
			"array": &ir.Node{
				Type: ir.ArrayType,
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
			name: ".array(string) - parameterized array reference",
			def: &ir.Node{
				Tag: ".array(string)",
			},
			want: reflect.SliceOf(reflect.TypeOf("")), // []string
		},
		{
			name: ".array(.[string]) - parameterized array with reference syntax",
			def: &ir.Node{
				Tag: ".array(.[string])",
			},
			want: reflect.SliceOf(reflect.TypeOf("")), // []string
		},
		{
			name: ".array(number) - parameterized array with number",
			def: &ir.Node{
				Tag: ".array(number)",
			},
			want: reflect.SliceOf(reflect.TypeOf(float64(0))), // []float64
		},
		{
			name: ".sparsearray(string) - parameterized sparse array",
			def: &ir.Node{
				Tag: ".sparsearray(string)",
			},
			want: reflect.MapOf(reflect.TypeOf(0), reflect.TypeOf("")), // map[int]string
		},
		{
			name: ".array(.array(string)) - nested parameterized arrays",
			def: &ir.Node{
				Tag: ".array(.array(string))",
			},
			want: reflect.SliceOf(reflect.SliceOf(reflect.TypeOf(""))), // [][]string
		},
		{
			name: "string value with .array(string)",
			def: &ir.Node{
				Type:   ir.StringType,
				String: ".array(string)",
			},
			want: reflect.SliceOf(reflect.TypeOf("")), // []string
		},
		{
			name: "invalid parameterized reference - missing closing paren",
			def: &ir.Node{
				Tag: ".array(string",
			},
			wantErr: true,
		},
		{
			name: "unknown constructor",
			def: &ir.Node{
				Tag: ".unknown(string)",
			},
			wantErr: true,
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
			if got != tt.want {
				t.Errorf("ExtractGoType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractGoType_ParameterizedDefinitions(t *testing.T) {
	s := &schema.Schema{
		Context: schema.DefaultContext(),
		Define: map[string]*ir.Node{
			"array": &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!type",
			},
			"string": &ir.Node{
				Type: ir.StringType,
				Tag:  "!type",
			},
			"array(t)": &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!and",
				Values: []*ir.Node{
					&ir.Node{Tag: ".[array]"}, // Base type
					&ir.Node{
						Tag: "!all.type t", // Parameter constraint
					},
				},
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
			name: "array(t) definition - extracts base array type",
			def: &ir.Node{
				Tag: ".[array(t)]",
			},
			want: reflect.TypeOf([]interface{}(nil)), // Base array type (can't resolve parameter here)
		},
		{
			name: ".array(string) usage - extracts []string",
			def: &ir.Node{
				Tag: ".array(string)",
			},
			want: reflect.SliceOf(reflect.TypeOf("")), // []string
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
			if got != tt.want {
				t.Errorf("ExtractGoType() = %v, want %v", got, tt.want)
			}
		})
	}
}
