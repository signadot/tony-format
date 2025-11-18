package gomap

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/schema"
)

func TestIsNullable(t *testing.T) {
	s := &schema.Schema{
		Context: schema.DefaultContext(),
		Define: map[string]*ir.Node{
			"string": &ir.Node{
				Type: ir.StringType,
				Tag:  "!irtype",
			},
			"nullable-string": &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!or",
				Values: []*ir.Node{
					ir.Null(),
					&ir.Node{Type: ir.StringType, Tag: "!irtype"},
				},
			},
			"non-nullable-string": &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!or",
				Values: []*ir.Node{
					&ir.Node{Type: ir.StringType, Tag: "!irtype"},
				},
			},
		},
	}

	tests := []struct {
		name string
		def  *ir.Node
		want bool
	}{
		{
			name: "!or [null, string] - nullable",
			def: &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!or",
				Values: []*ir.Node{
					ir.Null(),
					&ir.Node{Type: ir.StringType, Tag: "!irtype"},
				},
			},
			want: true,
		},
		{
			name: "!or [string] - not nullable",
			def: &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!or",
				Values: []*ir.Node{
					&ir.Node{Type: ir.StringType, Tag: "!irtype"},
				},
			},
			want: false,
		},
		{
			name: "string type - not nullable",
			def: &ir.Node{
				Type: ir.StringType,
				Tag:  "!irtype",
			},
			want: false,
		},
		{
			name: "!or [null, string] with null tag",
			def: &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!or",
				Values: []*ir.Node{
					&ir.Node{Tag: "null"},
					&ir.Node{Type: ir.StringType, Tag: "!irtype"},
				},
			},
			want: true,
		},
		{
			name: "!or [null, string] with null string",
			def: &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!or",
				Values: []*ir.Node{
					&ir.Node{Type: ir.StringType, String: "null"},
					&ir.Node{Type: ir.StringType, Tag: "!irtype"},
				},
			},
			want: true,
		},
		{
			name: "reference to nullable definition",
			def: &ir.Node{
				Tag: ".[nullable-string]",
			},
			want: true,
		},
		{
			name: "reference to non-nullable definition",
			def: &ir.Node{
				Tag: ".[string]",
			},
			want: false,
		},
		{
			name: "reference to non-nullable !or definition",
			def: &ir.Node{
				Tag: ".[non-nullable-string]",
			},
			want: false,
		},
		{
			name: "nil node - not nullable",
			def:  nil,
			want: false,
		},
		{
			name: "!or with multiple non-null types - not nullable",
			def: &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!or",
				Values: []*ir.Node{
					&ir.Node{Type: ir.StringType, Tag: "!irtype"},
					&ir.Node{Type: ir.NumberType, Tag: "!irtype"},
				},
			},
			want: false,
		},
		{
			name: "!or [null, string, number] - nullable",
			def: &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!or",
				Values: []*ir.Node{
					ir.Null(),
					&ir.Node{Type: ir.StringType, Tag: "!irtype"},
					&ir.Node{Type: ir.NumberType, Tag: "!irtype"},
				},
			},
			want: true,
		},
		{
			name: "string value reference",
			def: &ir.Node{
				Type:   ir.StringType,
				String: ".[nullable-string]",
			},
			want: true,
		},
		{
			name: "invalid reference - not nullable",
			def: &ir.Node{
				Tag: ".[nonexistent]",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsNullable(tt.def, s, nil)
			if got != tt.want {
				t.Errorf("IsNullable() = %v, want %v", got, tt.want)
			}
		})
	}
}
