package schema

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func TestParseSchemaReference(t *testing.T) {
	tests := []struct {
		name    string
		tag     string
		want    *SchemaReference
		wantErr bool
	}{
		{
			name: "simple schema name",
			tag:  "!schema(example)",
			want: &SchemaReference{
				Name: "example",
			},
		},
		{
			name: "schema URI with colon",
			tag:  "!schema(tony-format:schema:base)",
			want: &SchemaReference{
				URI: "tony-format:schema:base",
			},
		},
		{
			name: "parameterized schema",
			tag:  "!schema(p(1,2,3))",
			want: &SchemaReference{
				Name: "p",
				Args: []*ir.Node{
					ir.FromString("1"),
					ir.FromString("2"),
					ir.FromString("3"),
				},
			},
		},
		{
			name:    "missing tag",
			tag:     "",
			wantErr: true,
		},
		{
			name:    "wrong tag",
			tag:     "!other(example)",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &ir.Node{
				Tag: tt.tag,
			}
			got, err := ParseSchemaReference(node)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSchemaReference() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got.Name != tt.want.Name {
				t.Errorf("ParseSchemaReference() Name = %v, want %v", got.Name, tt.want.Name)
			}
			if got.URI != tt.want.URI {
				t.Errorf("ParseSchemaReference() URI = %v, want %v", got.URI, tt.want.URI)
			}
			if len(got.Args) != len(tt.want.Args) {
				t.Errorf("ParseSchemaReference() Args length = %v, want %v", len(got.Args), len(tt.want.Args))
			}
		})
	}
}

func TestParseFromReference(t *testing.T) {
	tests := []struct {
		name    string
		tag     string
		want    *FromReference
		wantErr bool
	}{
		{
			name: "simple from reference",
			tag:  "!from(base-schema,number)",
			want: &FromReference{
				SchemaName: "base-schema",
				DefName:    "number",
			},
		},
		{
			name: "parameterized schema",
			tag:  "!from(param-schema(1,2),def-name)",
			want: &FromReference{
				SchemaName: "param-schema",
				DefName:    "def-name",
				SchemaArgs: []*ir.Node{
					ir.FromString("1"),
					ir.FromString("2"),
				},
			},
		},
		{
			name:    "missing tag",
			tag:     "",
			wantErr: true,
		},
		{
			name:    "wrong tag",
			tag:     "!schema(base-schema, number)",
			wantErr: true,
		},
		{
			name:    "insufficient args",
			tag:     "!from(base-schema)",
			wantErr: true,
		},
		{
			name:    "space after comma",
			tag:     "!from(base-schema, number)",
			wantErr: true,
		},
		{
			name:    "space before comma",
			tag:     "!from(base-schema ,number)",
			wantErr: true,
		},
		{
			name:    "leading space in schema name",
			tag:     "!from( base-schema,number)",
			wantErr: true,
		},
		{
			name:    "trailing space in def name",
			tag:     "!from(base-schema,number )",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &ir.Node{
				Tag: tt.tag,
			}
			got, err := ParseFromReference(node)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFromReference() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got.SchemaName != tt.want.SchemaName {
				t.Errorf("ParseFromReference() SchemaName = %v, want %v", got.SchemaName, tt.want.SchemaName)
			}
			if got.DefName != tt.want.DefName {
				t.Errorf("ParseFromReference() DefName = %v, want %v", got.DefName, tt.want.DefName)
			}
			if len(got.SchemaArgs) != len(tt.want.SchemaArgs) {
				t.Errorf("ParseFromReference() SchemaArgs length = %v, want %v", len(got.SchemaArgs), len(tt.want.SchemaArgs))
			}
		})
	}
}

func TestResolveDefinition(t *testing.T) {
	// Create a context registry
	ctxReg := NewContextRegistry()

	// Create a schema registry
	schemaReg := NewSchemaRegistry(ctxReg)

	// Create a test schema with definitions
	testSchema := &Schema{
		Context: DefaultContext(),
		Signature: &Signature{
			Name: "base-schema",
		},
		Define: map[string]*ir.Node{
			"number": ir.FromInt(42),
			"string": ir.FromString("test"),
		},
	}

	// Register the schema
	if err := schemaReg.RegisterSchema(testSchema); err != nil {
		t.Fatalf("Failed to register schema: %v", err)
	}

	tests := []struct {
		name    string
		ref     *FromReference
		want    *ir.Node
		wantErr bool
	}{
		{
			name: "resolve existing definition",
			ref: &FromReference{
				SchemaName: "base-schema",
				DefName:    "number",
			},
			want: ir.FromInt(42),
		},
		{
			name: "resolve another definition",
			ref: &FromReference{
				SchemaName: "base-schema",
				DefName:    "string",
			},
			want: ir.FromString("test"),
		},
		{
			name: "non-existent schema",
			ref: &FromReference{
				SchemaName: "non-existent",
				DefName:    "number",
			},
			wantErr: true,
		},
		{
			name: "non-existent definition",
			ref: &FromReference{
				SchemaName: "base-schema",
				DefName:    "non-existent",
			},
			wantErr: true,
		},
		{
			name:    "nil reference",
			ref:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := schemaReg.ResolveDefinition(tt.ref)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveDefinition() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got == nil {
				t.Errorf("ResolveDefinition() returned nil")
				return
			}
			if tt.want != nil {
				// Compare the values
				if got.Type != tt.want.Type {
					t.Errorf("ResolveDefinition() Type = %v, want %v", got.Type, tt.want.Type)
				}
				if got.Type == ir.NumberType && got.Int64 != nil && tt.want.Int64 != nil {
					if *got.Int64 != *tt.want.Int64 {
						t.Errorf("ResolveDefinition() Int64 = %v, want %v", *got.Int64, *tt.want.Int64)
					}
				}
				if got.Type == ir.StringType && got.String != tt.want.String {
					t.Errorf("ResolveDefinition() String = %v, want %v", got.String, tt.want.String)
				}
			}
		})
	}
}

func TestSchemaTagAutoInjection(t *testing.T) {
	// Test that signature.name is automatically added to Tags when parsing
	schemaYAML := `
signature:
  name: example-schema
tags:
  other-tag:
    description: "Another tag"
`

	node, err := parse.Parse([]byte(schemaYAML))
	if err != nil {
		t.Fatalf("Failed to parse schema: %v", err)
	}

	schema, err := ParseSchema(node)
	if err != nil {
		t.Fatalf("Failed to parse schema: %v", err)
	}

	// Check that signature.name tag was auto-injected
	if schema.Tags == nil {
		t.Fatal("Tags map is nil")
	}

	// Should have both the auto-injected tag and the explicit tag
	if len(schema.Tags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(schema.Tags))
	}

	// Check auto-injected tag exists
	if tag, exists := schema.Tags["example-schema"]; !exists {
		t.Error("Auto-injected tag 'example-schema' not found")
	} else if tag.Name != "example-schema" {
		t.Errorf("Auto-injected tag name = %v, want 'example-schema'", tag.Name)
	}

	// Check explicit tag exists
	if tag, exists := schema.Tags["other-tag"]; !exists {
		t.Error("Explicit tag 'other-tag' not found")
	} else if tag.Description != "Another tag" {
		t.Errorf("Explicit tag description = %v, want 'Another tag'", tag.Description)
	}
}

func TestSchemaToIRElidesAutoInjectedTags(t *testing.T) {
	// Create a schema with signature.name that will be auto-injected
	schema := &Schema{
		Context: DefaultContext(),
		Signature: &Signature{
			Name: "example-schema",
		},
		Tags: map[string]*TagDefinition{
			"example-schema": {
				Name: "example-schema",
				// No additional fields - should be elided
			},
			"other-tag": {
				Name:        "other-tag",
				Description: "Another tag",
			},
		},
	}

	irNode, err := schema.ToIR()
	if err != nil {
		t.Fatalf("ToIR() error = %v", err)
	}

	// Get the tags field
	tagsNode := ir.Get(irNode, "tags")
	if tagsNode == nil {
		t.Fatal("tags field not found in IR")
	}

	// Check that auto-injected tag is elided
	tagsMap := make(map[string]bool)
	for i := range tagsNode.Fields {
		tagName := tagsNode.Fields[i].String
		tagsMap[tagName] = true
	}

	if tagsMap["example-schema"] {
		t.Error("Auto-injected tag 'example-schema' should be elided but was found")
	}

	if !tagsMap["other-tag"] {
		t.Error("Explicit tag 'other-tag' should be present but was not found")
	}
}

func TestContextToIRFromIRRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "simple URI",
			input: `"tony-format/context/match"`,
		},
		{
			name:  "object mapping",
			input: `{"match": "tony-format/context/match", "patch": "tony-format/context/patch"}`,
		},
		{
			name:  "array of contexts",
			input: `["tony-format/context/match", {"eval": "tony-format/context/eval"}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse input
			node, err := parse.Parse([]byte(tt.input))
			if err != nil {
				t.Fatalf("Failed to parse input: %v", err)
			}

			// Create context and parse from IR
			ctx := &Context{}
			if err := ctx.FromIR(node); err != nil {
				t.Fatalf("FromIR() error = %v", err)
			}

			// Convert back to IR
			irNode, err := ctx.ToIR()
			if err != nil {
				t.Fatalf("ToIR() error = %v", err)
			}

			// Verify consistency
			if irNode == nil {
				t.Fatal("ToIR() returned nil")
			}

			// Verify InOut and OutIn are consistent
			for term, uri := range ctx.InOut {
				if ctx.OutIn[uri] == nil || !ctx.OutIn[uri][term] {
					t.Errorf("InOut[%q] = %q but OutIn[%q][%q] is not true", term, uri, uri, term)
				}
			}

			for uri, terms := range ctx.OutIn {
				for term := range terms {
					if ctx.InOut[term] != uri {
						t.Errorf("OutIn[%q][%q] = true but InOut[%q] = %q (expected %q)", uri, term, term, ctx.InOut[term], uri)
					}
				}
			}
		})
	}
}

func TestContextInOutOutInConsistency(t *testing.T) {
	// Test that FromIR maintains consistency
	ctx := &Context{}
	node, err := parse.Parse([]byte(`{"match": "tony-format/context/match", "patch": "tony-format/context/patch"}`))
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if err := ctx.FromIR(node); err != nil {
		t.Fatalf("FromIR() error = %v", err)
	}

	// Verify consistency
	for term, uri := range ctx.InOut {
		if ctx.OutIn[uri] == nil || !ctx.OutIn[uri][term] {
			t.Errorf("InOut[%q] = %q but OutIn[%q][%q] is not true", term, uri, uri, term)
		}
	}

	for uri, terms := range ctx.OutIn {
		for term := range terms {
			if ctx.InOut[term] != uri {
				t.Errorf("OutIn[%q][%q] = true but InOut[%q] = %q (expected %q)", uri, term, term, ctx.InOut[term], uri)
			}
		}
	}
}

func TestSchemaValidate(t *testing.T) {
	tests := []struct {
		name    string
		schema  *Schema
		doc     *ir.Node
		wantErr bool
	}{
		{
			name: "nil accept accepts everything",
			schema: &Schema{
				Accept: nil,
			},
			doc:     ir.FromString("anything"),
			wantErr: false,
		},
		{
			name: "irtype string matches string",
			schema: &Schema{
				Define: map[string]*ir.Node{
					"string": ir.FromString("").WithTag("!irtype"),
				},
				Accept: ir.FromString(".[string]"),
			},
			doc:     ir.FromString("hello"),
			wantErr: false,
		},
		{
			name: "irtype string rejects number",
			schema: &Schema{
				Define: map[string]*ir.Node{
					"string": ir.FromString("").WithTag("!irtype"),
				},
				Accept: ir.FromString(".[string]"),
			},
			doc:     ir.FromInt(42),
			wantErr: true,
		},
		{
			name: "irtype array matches array",
			schema: &Schema{
				Define: map[string]*ir.Node{
					"array": ir.FromSlice([]*ir.Node{}).WithTag("!irtype"),
				},
				Accept: ir.FromString(".[array]"),
			},
			doc:     ir.FromSlice([]*ir.Node{ir.FromInt(1), ir.FromInt(2)}),
			wantErr: false,
		},
		{
			name: "and combinator with def refs",
			schema: &Schema{
				Define: map[string]*ir.Node{
					"array": ir.FromSlice([]*ir.Node{}).WithTag("!irtype"),
					"myarray": ir.FromSlice([]*ir.Node{
						ir.FromString(".[array]"),
					}).WithTag("!and"),
				},
				Accept: ir.FromString(".[myarray]"),
			},
			doc:     ir.FromSlice([]*ir.Node{ir.FromInt(1), ir.FromInt(2)}),
			wantErr: false,
		},
		{
			name: "or combinator matches first branch",
			schema: &Schema{
				Define: map[string]*ir.Node{
					"string": ir.FromString("").WithTag("!irtype"),
					"number": ir.FromInt(0).WithTag("!irtype"),
					"stringornumber": ir.FromSlice([]*ir.Node{
						ir.FromString(".[string]"),
						ir.FromString(".[number]"),
					}).WithTag("!or"),
				},
				Accept: ir.FromString(".[stringornumber]"),
			},
			doc:     ir.FromString("hello"),
			wantErr: false,
		},
		{
			name: "or combinator matches second branch",
			schema: &Schema{
				Define: map[string]*ir.Node{
					"string": ir.FromString("").WithTag("!irtype"),
					"number": ir.FromInt(0).WithTag("!irtype"),
					"stringornumber": ir.FromSlice([]*ir.Node{
						ir.FromString(".[string]"),
						ir.FromString(".[number]"),
					}).WithTag("!or"),
				},
				Accept: ir.FromString(".[stringornumber]"),
			},
			doc:     ir.FromInt(42),
			wantErr: false,
		},
		{
			name: "or combinator rejects non-matching",
			schema: &Schema{
				Define: map[string]*ir.Node{
					"string": ir.FromString("").WithTag("!irtype"),
					"number": ir.FromInt(0).WithTag("!irtype"),
					"stringornumber": ir.FromSlice([]*ir.Node{
						ir.FromString(".[string]"),
						ir.FromString(".[number]"),
					}).WithTag("!or"),
				},
				Accept: ir.FromString(".[stringornumber]"),
			},
			doc:     ir.FromSlice([]*ir.Node{}),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.schema.Validate(tt.doc)
			if (err != nil) != tt.wantErr {
				t.Errorf("Schema.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
