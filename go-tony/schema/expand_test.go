package schema

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

func TestBuildEvalOptions(t *testing.T) {
	tests := []struct {
		name     string
		define   map[string]*ir.Node
		wantDefs []string // expected parameterized def names
	}{
		{
			name:     "nil schema",
			define:   nil,
			wantDefs: nil,
		},
		{
			name: "no parameterized defs",
			define: map[string]*ir.Node{
				"number": ir.FromString("number-def"),
				"string": ir.FromString("string-def"),
			},
			wantDefs: nil,
		},
		{
			name: "one parameterized def",
			define: map[string]*ir.Node{
				"number":   ir.FromString("number-def"),
				"array(t)": ir.FromString("array-def"),
			},
			wantDefs: []string{"array"},
		},
		{
			name: "multiple parameterized defs",
			define: map[string]*ir.Node{
				"array(t)":    ir.FromString("array-def"),
				"nullable(t)": ir.FromString("nullable-def"),
				"map(k,v)":    ir.FromString("map-def"),
				"number":      ir.FromString("number-def"),
			},
			wantDefs: []string{"array", "nullable", "map"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Schema{Define: tt.define}
			opts := BuildEvalOptions(s)

			if tt.wantDefs == nil {
				if opts != nil {
					t.Errorf("expected nil opts, got %v", opts)
				}
				return
			}

			if opts == nil {
				t.Fatalf("expected non-nil opts")
			}

			for _, name := range tt.wantDefs {
				if !opts.ParameterizedDefs[name] {
					t.Errorf("expected %q in ParameterizedDefs", name)
				}
			}

			if len(opts.ParameterizedDefs) != len(tt.wantDefs) {
				t.Errorf("got %d parameterized defs, want %d", len(opts.ParameterizedDefs), len(tt.wantDefs))
			}
		})
	}
}

func TestBuildDefEnv(t *testing.T) {
	s := &Schema{
		Define: map[string]*ir.Node{
			"number":   ir.FromString("number-def"),
			"array(t)": ir.FromString("array-base"),
		},
	}

	env := BuildDefEnv(s)

	// Check non-parameterized def is stored as IR node
	numberDef, ok := env["number"].(*ir.Node)
	if !ok {
		t.Fatalf("number should be *ir.Node, got %T", env["number"])
	}
	if numberDef.String != "number-def" {
		t.Errorf("number def = %q, want %q", numberDef.String, "number-def")
	}

	// Check parameterized def is stored as function
	arrayFunc, ok := env["array"].(func(...any) any)
	if !ok {
		t.Fatalf("array should be func(...any) any, got %T", env["array"])
	}

	// Call with no args - should return base definition
	result := arrayFunc()
	resultNode, ok := result.(*ir.Node)
	if !ok {
		t.Fatalf("array() should return *ir.Node, got %T", result)
	}
	if resultNode.String != "array-base" {
		t.Errorf("array() = %q, want %q", resultNode.String, "array-base")
	}
}

func TestExpandDefBody_Integration(t *testing.T) {
	// Create a schema with parameterized and non-parameterized defs
	s := &Schema{
		Define: map[string]*ir.Node{
			"number":   ir.FromString("number-def"),
			"array(t)": ir.FromString("array-base"),
		},
	}

	env := BuildDefEnv(s)
	opts := BuildEvalOptions(s)

	tests := []struct {
		name  string
		input *ir.Node
		want  string
	}{
		{
			name:  "reference to non-parameterized def",
			input: ir.FromString(".[number]"),
			want:  "number-def",
		},
		{
			name:  "reference to parameterized def (auto-call)",
			input: ir.FromString(".[array]"),
			want:  "array-base",
		},
		{
			name:  "explicit call to parameterized def",
			input: ir.FromString(".[array()]"),
			want:  "array-base",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExpandDefBody(tt.input, env, opts)
			if err != nil {
				t.Fatalf("ExpandDefBody error: %v", err)
			}
			if result.String != tt.want {
				t.Errorf("got %q, want %q", result.String, tt.want)
			}
		})
	}
}
