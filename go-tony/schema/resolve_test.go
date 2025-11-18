package schema

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

func TestResolveDefinitionName(t *testing.T) {
	// Create a test schema with definitions
	testSchema := &Schema{
		Context: DefaultContext(),
		Define: map[string]*ir.Node{
			"number": ir.FromInt(42),
			"string": ir.FromString("hello"),
			"bool":   ir.FromBool(true),
			"int": &ir.Node{
				Type: ir.ObjectType,
				Tag:  "!and",
				Fields: []*ir.Node{
					ir.FromString("number"),
				},
				Values: []*ir.Node{
					&ir.Node{Tag: ".[number]"}, // Reference to "number" definition
				},
			},
		},
	}

	tests := []struct {
		name    string
		schema  *Schema
		defName string
		want    *ir.Node
		wantErr bool
		errMsg  string
	}{
		{
			name:    "resolve existing definition - number",
			schema:  testSchema,
			defName: "number",
			want:    ir.FromInt(42),
			wantErr: false,
		},
		{
			name:    "resolve existing definition - string",
			schema:  testSchema,
			defName: "string",
			want:    ir.FromString("hello"),
			wantErr: false,
		},
		{
			name:    "resolve existing definition - bool",
			schema:  testSchema,
			defName: "bool",
			want:    ir.FromBool(true),
			wantErr: false,
		},
		{
			name:    "resolve existing definition - complex node",
			schema:  testSchema,
			defName: "int",
			want:    testSchema.Define["int"],
			wantErr: false,
		},
		{
			name:    "non-existent definition",
			schema:  testSchema,
			defName: "non-existent",
			want:    nil,
			wantErr: true,
			errMsg:  "definition \"non-existent\" not found in schema",
		},
		{
			name:    "empty definition name",
			schema:  testSchema,
			defName: "",
			want:    nil,
			wantErr: true,
			errMsg:  "definition name cannot be empty",
		},
		{
			name:    "nil schema",
			schema:  nil,
			defName: "number",
			want:    nil,
			wantErr: true,
			errMsg:  "schema cannot be nil",
		},
		{
			name: "schema with nil Define map",
			schema: &Schema{
				Context: DefaultContext(),
				Define:  nil,
			},
			defName: "number",
			want:    nil,
			wantErr: true,
			errMsg:  "schema has no definitions",
		},
		{
			name: "schema with empty Define map",
			schema: &Schema{
				Context: DefaultContext(),
				Define:  make(map[string]*ir.Node),
			},
			defName: "number",
			want:    nil,
			wantErr: true,
			errMsg:  "definition \"number\" not found in schema",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveDefinitionName(tt.schema, tt.defName)

			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveDefinitionName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				if err == nil {
					t.Error("ResolveDefinitionName() expected error but got nil")
					return
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("ResolveDefinitionName() error message = %q, want %q", err.Error(), tt.errMsg)
				}
				return
			}

			if got == nil {
				t.Error("ResolveDefinitionName() returned nil but expected a node")
				return
			}

			if tt.want == nil {
				t.Error("test case has nil want but no error expected")
				return
			}

			// Compare the nodes based on their types
			if got.Type != tt.want.Type {
				t.Errorf("ResolveDefinitionName() Type = %v, want %v", got.Type, tt.want.Type)
			}

			// Compare values based on type
			switch got.Type {
			case ir.NumberType:
				if got.Int64 != nil && tt.want.Int64 != nil {
					if *got.Int64 != *tt.want.Int64 {
						t.Errorf("ResolveDefinitionName() Int64 = %v, want %v", *got.Int64, *tt.want.Int64)
					}
				} else if (got.Int64 == nil) != (tt.want.Int64 == nil) {
					t.Errorf("ResolveDefinitionName() Int64 = %v, want %v", got.Int64, tt.want.Int64)
				}
			case ir.StringType:
				if got.String != tt.want.String {
					t.Errorf("ResolveDefinitionName() String = %q, want %q", got.String, tt.want.String)
				}
			case ir.BoolType:
				if got.Bool != tt.want.Bool {
					t.Errorf("ResolveDefinitionName() Bool = %v, want %v", got.Bool, tt.want.Bool)
				}
			case ir.ObjectType:
				// For complex nodes, just verify they're the same reference
				if got != tt.want {
					// Check if they have the same structure
					if len(got.Fields) != len(tt.want.Fields) {
						t.Errorf("ResolveDefinitionName() Fields length = %v, want %v", len(got.Fields), len(tt.want.Fields))
					}
					if got.Tag != tt.want.Tag {
						t.Errorf("ResolveDefinitionName() Tag = %q, want %q", got.Tag, tt.want.Tag)
					}
				}
			}
		})
	}
}

func TestResolveDefinitionNameWithRealSchema(t *testing.T) {
	// Test with a schema that mimics real-world usage
	schema := &Schema{
		Context: DefaultContext(),
		Define: map[string]*ir.Node{
			"number": &ir.Node{
				Type: ir.NumberType,
				Tag:  "!irtype",
			},
			"int": &ir.Node{
				Type: ir.ObjectType,
				Tag:  "!and",
				Fields: []*ir.Node{
					ir.FromString("number"),
					ir.FromString("int"),
				},
				Values: []*ir.Node{
					&ir.Node{Tag: ".[number]"}, // Reference
					&ir.Node{
						Type: ir.ObjectType,
						Fields: []*ir.Node{ir.FromString("int")},
						Values: []*ir.Node{&ir.Node{Tag: "!not null"}},
					},
				},
			},
			"array": &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!irtype",
			},
			"array(t)": &ir.Node{
				Type: ir.ObjectType,
				Tag:  "!and",
				Fields: []*ir.Node{
					ir.FromString("array"),
				},
				Values: []*ir.Node{
					&ir.Node{Tag: ".[array]"}, // Reference to "array"
				},
			},
		},
	}

	// Test resolving "number"
	numberDef, err := ResolveDefinitionName(schema, "number")
	if err != nil {
		t.Fatalf("ResolveDefinitionName(\"number\") error = %v", err)
	}
	if numberDef == nil {
		t.Fatal("ResolveDefinitionName(\"number\") returned nil")
	}
	if numberDef.Tag != "!irtype" {
		t.Errorf("ResolveDefinitionName(\"number\") Tag = %q, want %q", numberDef.Tag, "!irtype")
	}

	// Test resolving "int" (which contains a reference to "number")
	intDef, err := ResolveDefinitionName(schema, "int")
	if err != nil {
		t.Fatalf("ResolveDefinitionName(\"int\") error = %v", err)
	}
	if intDef == nil {
		t.Fatal("ResolveDefinitionName(\"int\") returned nil")
	}
	if intDef.Tag != "!and" {
		t.Errorf("ResolveDefinitionName(\"int\") Tag = %q, want %q", intDef.Tag, "!and")
	}
	// Verify it contains a reference to ".[number]"
	if len(intDef.Values) < 1 {
		t.Fatal("ResolveDefinitionName(\"int\") Values is empty")
	}
	if intDef.Values[0].Tag != ".[number]" {
		t.Errorf("ResolveDefinitionName(\"int\") first value Tag = %q, want %q", intDef.Values[0].Tag, ".[number]")
	}

	// Test resolving "array"
	arrayDef, err := ResolveDefinitionName(schema, "array")
	if err != nil {
		t.Fatalf("ResolveDefinitionName(\"array\") error = %v", err)
	}
	if arrayDef == nil {
		t.Fatal("ResolveDefinitionName(\"array\") returned nil")
	}

	// Test resolving parameterized definition "array(t)"
	arrayTDef, err := ResolveDefinitionName(schema, "array(t)")
	if err != nil {
		t.Fatalf("ResolveDefinitionName(\"array(t)\") error = %v", err)
	}
	if arrayTDef == nil {
		t.Fatal("ResolveDefinitionName(\"array(t)\") returned nil")
	}
	// Verify it contains a reference to ".[array]"
	if len(arrayTDef.Values) < 1 {
		t.Fatal("ResolveDefinitionName(\"array(t)\") Values is empty")
	}
	if arrayTDef.Values[0].Tag != ".[array]" {
		t.Errorf("ResolveDefinitionName(\"array(t)\") first value Tag = %q, want %q", arrayTDef.Values[0].Tag, ".[array]")
	}
}

func TestResolveDefinitionNameCaseSensitivity(t *testing.T) {
	schema := &Schema{
		Context: DefaultContext(),
		Define: map[string]*ir.Node{
			"Number": ir.FromInt(42),
			"number": ir.FromInt(100),
		},
	}

	// Test case-sensitive lookup
	upperDef, err := ResolveDefinitionName(schema, "Number")
	if err != nil {
		t.Fatalf("ResolveDefinitionName(\"Number\") error = %v", err)
	}
	if upperDef.Int64 == nil || *upperDef.Int64 != 42 {
		t.Errorf("ResolveDefinitionName(\"Number\") = %v, want 42", upperDef.Int64)
	}

	lowerDef, err := ResolveDefinitionName(schema, "number")
	if err != nil {
		t.Fatalf("ResolveDefinitionName(\"number\") error = %v", err)
	}
	if lowerDef.Int64 == nil || *lowerDef.Int64 != 100 {
		t.Errorf("ResolveDefinitionName(\"number\") = %v, want 100", lowerDef.Int64)
	}

	// Verify they're different
	if upperDef == lowerDef {
		t.Error("ResolveDefinitionName should be case-sensitive")
	}
}
