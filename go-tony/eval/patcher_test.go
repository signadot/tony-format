package eval

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

func TestDefCallPatcher(t *testing.T) {
	// Test that bare identifiers for parameterized defs get transformed to function calls
	tests := []struct {
		name             string
		expr             string
		parameterizedDef string
		env              map[string]any
		want             any
	}{
		{
			name:             "bare identifier transformed to zero-arg call",
			expr:             "array",
			parameterizedDef: "array",
			env: map[string]any{
				// array is a function that returns the base def when called with no args
				"array": func(args ...any) any {
					if len(args) == 0 {
						return ir.FromString("base-array-def")
					}
					return ir.FromString("instantiated-with-args")
				},
			},
			want: "base-array-def",
		},
		{
			name:             "explicit call with zero args is not double-transformed",
			expr:             "array()",
			parameterizedDef: "array",
			env: map[string]any{
				"array": func(args ...any) any {
					return ir.FromString("called-with-zero-args")
				},
			},
			want: "called-with-zero-args",
		},
		{
			name:             "non-parameterized def is not transformed",
			expr:             "number",
			parameterizedDef: "array", // array is parameterized, but we're accessing number
			env: map[string]any{
				"number": ir.FromString("number-def"),
			},
			want: "number-def",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &EvalOptions{
				ParameterizedDefs: map[string]bool{tt.parameterizedDef: true},
			}
			result, err := evalWithOptions(tt.expr, tt.env, opts)
			if err != nil {
				t.Fatalf("evalWithOptions error: %v", err)
			}

			// Check the result
			switch want := tt.want.(type) {
			case string:
				node, ok := result.(*ir.Node)
				if !ok {
					t.Fatalf("expected *ir.Node, got %T", result)
				}
				if node.String != want {
					t.Errorf("got %q, want %q", node.String, want)
				}
			default:
				t.Fatalf("unexpected want type: %T", want)
			}
		})
	}
}

func TestExpandIRWithOptions_ParameterizedDefs(t *testing.T) {
	// Test the full flow: ExpandIRWithOptions with parameterized defs
	// This simulates how schema expansion would work

	// Create a mock parameterized def function
	arrayDef := ir.FromString("base-array-definition")
	arrayFunc := func(args ...any) any {
		if len(args) == 0 {
			return arrayDef.Clone()
		}
		// In real usage, this would call InstantiateDef
		return ir.FromString("instantiated-array")
	}

	env := map[string]any{
		"array":  arrayFunc,
		"number": ir.FromString("number-def"),
	}

	opts := &EvalOptions{
		ParameterizedDefs: map[string]bool{"array": true},
	}

	tests := []struct {
		name   string
		input  *ir.Node
		want   string
		useOpt bool
	}{
		{
			name:   "raw ref to parameterized def without args",
			input:  ir.FromString(".[array]"),
			want:   "base-array-definition",
			useOpt: true,
		},
		{
			name:   "raw ref to non-parameterized def",
			input:  ir.FromString(".[number]"),
			want:   "number-def",
			useOpt: true,
		},
		{
			name:   "without options, parameterized def needs explicit call",
			input:  ir.FromString(".[array()]"),
			want:   "base-array-definition",
			useOpt: false, // no patching
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result *ir.Node
			var err error

			if tt.useOpt {
				result, err = ExpandIRWithOptions(tt.input, env, opts)
			} else {
				result, err = ExpandIRWithOptions(tt.input, env, nil)
			}

			if err != nil {
				t.Fatalf("ExpandIRWithOptions error: %v", err)
			}

			if result.String != tt.want {
				t.Errorf("got %q, want %q", result.String, tt.want)
			}
		})
	}
}

func TestExpandStringWithOptions_ParameterizedDefs(t *testing.T) {
	// Test string expansion with parameterized defs embedded in strings

	arrayFunc := func(args ...any) any {
		if len(args) == 0 {
			return "base-array"
		}
		return "instantiated"
	}

	env := map[string]any{
		"array":  arrayFunc,
		"number": "42",
	}

	opts := &EvalOptions{
		ParameterizedDefs: map[string]bool{"array": true},
	}

	tests := []struct {
		name   string
		input  string
		want   string
		useOpt bool
	}{
		{
			name:   "embedded ref to parameterized def",
			input:  "type: .[array]",
			want:   "type: base-array",
			useOpt: true,
		},
		{
			name:   "embedded ref to regular value",
			input:  "count: .[number]",
			want:   "count: 42",
			useOpt: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result string
			var err error

			if tt.useOpt {
				result, err = expandStringWithOptions(tt.input, env, opts)
			} else {
				result, err = expandStringWithOptions(tt.input, env, nil)
			}

			if err != nil {
				t.Fatalf("expandStringWithOptions error: %v", err)
			}

			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}
