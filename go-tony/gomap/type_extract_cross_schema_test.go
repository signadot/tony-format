package gomap

import (
	"reflect"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/schema"
)

func TestExtractGoType_CrossSchemaReferences(t *testing.T) {
	// Create a registry with multiple schemas
	contextRegistry := schema.NewContextRegistry()
	registry := schema.NewSchemaRegistry(contextRegistry)

	// Create base schema with a number definition
	baseSchema := &schema.Schema{
		Context: schema.DefaultContext(),
		Signature: &schema.Signature{
			Name: "base-schema",
		},
		Define: map[string]*ir.Node{
			"number": &ir.Node{
				Type: ir.NumberType,
				Tag:  "!irtype",
			},
			"string": &ir.Node{
				Type: ir.StringType,
				Tag:  "!irtype",
			},
		},
		Accept: &ir.Node{
			Type: ir.ObjectType,
			Tag:  "!irtype",
		},
	}

	// Create derived schema that references base schema
	derivedSchema := &schema.Schema{
		Context: schema.DefaultContext(),
		Signature: &schema.Signature{
			Name: "derived-schema",
		},
		Define: map[string]*ir.Node{
			"my-number": &ir.Node{
				Tag: "!from(base-schema, number)",
			},
			"my-string": &ir.Node{
				Tag: "!from(base-schema, string)",
			},
		},
		Accept: &ir.Node{
			Type: ir.StringType,
			Tag:  "!irtype",
		},
	}

	// Register schemas
	if err := registry.RegisterSchema(baseSchema); err != nil {
		t.Fatalf("failed to register base schema: %v", err)
	}
	if err := registry.RegisterSchema(derivedSchema); err != nil {
		t.Fatalf("failed to register derived schema: %v", err)
	}

	tests := []struct {
		name         string
		def          *ir.Node
		s            *schema.Schema
		registry     *schema.SchemaRegistry
		useRegistry  bool // Whether to use registry (false means use nil)
		want         reflect.Type
		wantErr      bool
	}{
		{
			name:        "!from(base-schema, number) - cross-schema reference",
			def:         &ir.Node{Tag: "!from(base-schema, number)"},
			s:           derivedSchema,
			registry:    registry,
			useRegistry: true,
			want:        reflect.TypeOf(float64(0)),
		},
		{
			name:        "!from(base-schema, string) - cross-schema reference",
			def:         &ir.Node{Tag: "!from(base-schema, string)"},
			s:           derivedSchema,
			registry:    registry,
			useRegistry: true,
			want:        reflect.TypeOf(""),
		},
		{
			name:        "!schema(base-schema) - schema reference",
			def:         &ir.Node{Tag: "!schema(base-schema)"},
			s:           derivedSchema,
			registry:    registry,
			useRegistry: true,
			want:        reflect.TypeOf(map[string]interface{}(nil)), // base-schema.Accept is object type
		},
		{
			name:        "!schema(derived-schema) - schema reference",
			def:         &ir.Node{Tag: "!schema(derived-schema)"},
			s:           baseSchema,
			registry:    registry,
			useRegistry: true,
			want:        reflect.TypeOf(""), // derived-schema.Accept is string type
		},
		{
			name:        "!from with non-existent schema",
			def:         &ir.Node{Tag: "!from(nonexistent-schema, number)"},
			s:           derivedSchema,
			registry:    registry,
			useRegistry: true,
			wantErr:     true,
		},
		{
			name:        "!from with non-existent definition",
			def:         &ir.Node{Tag: "!from(base-schema, nonexistent)"},
			s:           derivedSchema,
			registry:    registry,
			useRegistry: true,
			wantErr:     true,
		},
		{
			name:        "!schema with non-existent schema",
			def:         &ir.Node{Tag: "!schema(nonexistent-schema)"},
			s:           derivedSchema,
			registry:    registry,
			useRegistry: true,
			wantErr:     true,
		},
		{
			name:        "!from without registry",
			def:         &ir.Node{Tag: "!from(base-schema, number)"},
			s:           derivedSchema,
			registry:    nil,
			useRegistry: false,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reg *schema.SchemaRegistry
			if tt.useRegistry {
				reg = tt.registry
			} else {
				reg = nil // Explicitly use nil
			}
			got, err := ExtractGoType(tt.def, tt.s, reg)
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

func TestExtractGoType_CrossSchemaWithNullable(t *testing.T) {
	// Create a registry with schemas
	contextRegistry := schema.NewContextRegistry()
	registry := schema.NewSchemaRegistry(contextRegistry)

	// Create base schema with nullable string
	baseSchema := &schema.Schema{
		Context: schema.DefaultContext(),
		Signature: &schema.Signature{
			Name: "base-schema",
		},
		Define: map[string]*ir.Node{
			"nullable-string": &ir.Node{
				Type: ir.ArrayType,
				Tag:  "!or",
				Values: []*ir.Node{
					ir.Null(),
					&ir.Node{Type: ir.StringType, Tag: "!irtype"},
				},
			},
		},
		Accept: &ir.Node{
			Type: ir.StringType,
			Tag:  "!irtype",
		},
	}

	// Create derived schema
	derivedSchema := &schema.Schema{
		Context: schema.DefaultContext(),
		Signature: &schema.Signature{
			Name: "derived-schema",
		},
		Define: map[string]*ir.Node{
			"email": &ir.Node{
				Tag: "!from(base-schema, nullable-string)",
			},
		},
		Accept: &ir.Node{
			Type: ir.StringType,
			Tag:  "!irtype",
		},
	}

	// Register schemas
	if err := registry.RegisterSchema(baseSchema); err != nil {
		t.Fatalf("failed to register base schema: %v", err)
	}
	if err := registry.RegisterSchema(derivedSchema); err != nil {
		t.Fatalf("failed to register derived schema: %v", err)
	}

	// Test cross-schema reference to nullable type
	def := &ir.Node{
		Tag: "!from(base-schema, nullable-string)",
	}

	got, err := ExtractGoType(def, derivedSchema, registry)
	if err != nil {
		t.Fatalf("ExtractGoType() error = %v", err)
	}

	want := reflect.PtrTo(reflect.TypeOf("")) // *string
	if got != want {
		t.Errorf("ExtractGoType() = %v, want %v", got, want)
	}
}
