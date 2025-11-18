package gomap

import (
	"reflect"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/schema"
)

func TestExtractGoType_BasicTypes(t *testing.T) {
	s := &schema.Schema{
		Context: schema.DefaultContext(),
		Define:  make(map[string]*ir.Node),
	}

	tests := []struct {
		name    string
		def     *ir.Node
		want    reflect.Type
		wantErr bool
	}{
		{
			name: "string type",
			def: &ir.Node{
				Type: ir.StringType,
				Tag:  "!type",
			},
			want: reflect.TypeOf(""),
		},
		{
			name: "number type",
			def: &ir.Node{
				Type: ir.NumberType,
				Tag:  "!type",
			},
			want: reflect.TypeOf(float64(0)),
		},
		{
			name: "bool type",
			def: &ir.Node{
				Type: ir.BoolType,
				Tag:  "!type",
			},
			want: reflect.TypeOf(false),
		},
		{
			name: "string value",
			def: &ir.Node{
				Type:   ir.StringType,
				String: "hello",
			},
			want: reflect.TypeOf(""),
		},
		{
			name: "!type string",
			def: &ir.Node{
				Type:   ir.StringType,
				String: "",
				Tag:    "!type",
			},
			want: reflect.TypeOf(""),
		},
		{
			name: "!type number",
			def: &ir.Node{
				Type:   ir.NumberType,
				Int64:  intPtr(1),
				Tag:    "!type",
			},
			want: reflect.TypeOf(float64(0)),
		},
		{
			name: "!type bool",
			def: &ir.Node{
				Type: ir.BoolType,
				Bool: true,
				Tag:  "!type",
			},
			want: reflect.TypeOf(false),
		},
		{
			name: "nil node",
			def:  nil,
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

func TestExtractGoType_References(t *testing.T) {
	s := &schema.Schema{
		Context: schema.DefaultContext(),
		Define: map[string]*ir.Node{
			"number": &ir.Node{
				Type: ir.NumberType,
				Tag:  "!type",
			},
			"string": &ir.Node{
				Type: ir.StringType,
				Tag:  "!type",
			},
			"int": &ir.Node{
				Type: ir.ObjectType,
				Tag:  "!and",
				Fields: []*ir.Node{ir.FromString("number")},
				Values: []*ir.Node{
					&ir.Node{Tag: ".[number]"}, // Reference
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
			name: "reference to number",
			def: &ir.Node{
				Tag: ".[number]",
			},
			want: reflect.TypeOf(float64(0)),
		},
		{
			name: "reference to string",
			def: &ir.Node{
				Tag: ".[string]",
			},
			want: reflect.TypeOf(""),
		},
		{
			name: "reference to int (which references number)",
			def: &ir.Node{
				Tag: ".[int]",
			},
			want: reflect.TypeOf(float64(0)), // int resolves to number
		},
		{
			name: "invalid reference",
			def: &ir.Node{
				Tag: ".[nonexistent]",
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

func TestExtractGoType_NullableTypes(t *testing.T) {
	s := &schema.Schema{
		Context: schema.DefaultContext(),
		Define: map[string]*ir.Node{
			"string": &ir.Node{
				Type: ir.StringType,
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
			name: "!or [null, string]",
			def: &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!or",
				Values: []*ir.Node{
					ir.Null(),
					&ir.Node{
						Type: ir.StringType,
						Tag:  "!type",
					},
				},
			},
			want: reflect.PtrTo(reflect.TypeOf("")), // *string
		},
		{
			name: "!or [null, reference]",
			def: &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!or",
				Values: []*ir.Node{
					ir.Null(),
					&ir.Node{Tag: ".[string]"},
				},
			},
			want: reflect.PtrTo(reflect.TypeOf("")), // *string
		},
		{
			name: "!or [string] (no null)",
			def: &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!or",
				Values: []*ir.Node{
					&ir.Node{
						Type: ir.StringType,
						Tag:  "!type",
					},
				},
			},
			want: reflect.TypeOf(""), // string (not pointer)
		},
		{
			name: "!or [null, string, int] (multiple non-null)",
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

func TestExtractGoType_Arrays(t *testing.T) {
	s := &schema.Schema{
		Context: schema.DefaultContext(),
		Define: map[string]*ir.Node{
			"string": &ir.Node{
				Type: ir.StringType,
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
			name: "array of strings",
			def: &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!type",
				Values: []*ir.Node{
					&ir.Node{Type: ir.StringType, Tag: "!type"},
				},
			},
			want: reflect.SliceOf(reflect.TypeOf("")), // []string
		},
		{
			name: "empty array",
			def: &ir.Node{
				Type:  ir.ArrayType,
				Tag:   "!type",
				Values: []*ir.Node{},
			},
			want: reflect.TypeOf([]interface{}(nil)), // []interface{} (default)
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

func TestExtractGoType_AndConstraints(t *testing.T) {
	s := &schema.Schema{
		Context: schema.DefaultContext(),
		Define: map[string]*ir.Node{
			"number": &ir.Node{
				Type: ir.NumberType,
				Tag:  "!type",
			},
			"string": &ir.Node{
				Type: ir.StringType,
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
			name: "!and with reference and !not null constraint",
			def: &ir.Node{
				Type: ir.ObjectType,
				Tag:  "!and",
				Fields: []*ir.Node{ir.FromString("number")},
				Values: []*ir.Node{
					&ir.Node{Tag: ".[number]"}, // Base type
					&ir.Node{
						Type: ir.ObjectType,
						Fields: []*ir.Node{ir.FromString("int")},
						Values: []*ir.Node{&ir.Node{Tag: "!not null"}}, // Constraint - should be skipped
					},
				},
			},
			want: reflect.TypeOf(float64(0)), // Extracts base type from reference
		},
		{
			name: "!and with multiple type references (uses first)",
			def: &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!and",
				Values: []*ir.Node{
					&ir.Node{Tag: ".[number]"}, // First reference
					&ir.Node{Tag: ".[string]"}, // Second reference (ignored)
				},
			},
			want: reflect.TypeOf(float64(0)), // Uses first extractable type
		},
		{
			name: "!and with !not constraint first (skips it)",
			def: &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!and",
				Values: []*ir.Node{
					&ir.Node{Tag: "!not null"}, // Constraint - should be skipped
					&ir.Node{Tag: ".[string]"}, // Base type - should be used
				},
			},
			want: reflect.TypeOf(""), // Extracts base type, skipping constraint
		},
		{
			name: "!and with only constraints (no extractable type)",
			def: &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!and",
				Values: []*ir.Node{
					&ir.Node{Tag: "!not null"},
					&ir.Node{Tag: "!not"},
				},
			},
			wantErr: true, // No extractable type found
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

func TestExtractGoType_ComplexTypes(t *testing.T) {
	s := &schema.Schema{
		Context: schema.DefaultContext(),
		Define: map[string]*ir.Node{
			"string": &ir.Node{
				Type: ir.StringType,
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
			name: "nullable array of strings",
			def: &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!or",
				Values: []*ir.Node{
					ir.Null(),
					&ir.Node{
						Type: ir.ArrayType,
						Tag:  "!type",
						Values: []*ir.Node{
							&ir.Node{Type: ir.StringType, Tag: "!type"},
						},
					},
				},
			},
			want: reflect.PtrTo(reflect.SliceOf(reflect.TypeOf(""))), // *[]string
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

func intPtr(i int64) *int64 {
	return &i
}
