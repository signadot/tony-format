package schema

import (
	"github.com/signadot/tony-format/go-tony/eval"
	"github.com/signadot/tony-format/go-tony/ir"
)

// BuildEvalOptions creates EvalOptions for expanding schema definition bodies.
// It extracts parameterized definition names from the schema's Define map
// so that bare references like .[array] are auto-called as array().
func BuildEvalOptions(s *Schema) *eval.EvalOptions {
	if s == nil || s.Define == nil {
		return nil
	}

	parameterizedDefs := make(map[string]bool)
	for defName := range s.Define {
		baseName, params := ParseDefSignature(defName)
		if len(params) > 0 {
			parameterizedDefs[baseName] = true
		}
	}

	if len(parameterizedDefs) == 0 {
		return nil
	}

	return &eval.EvalOptions{
		ParameterizedDefs: parameterizedDefs,
	}
}

// ExpandDefBody expands a definition body using the schema's definition environment.
// This handles parameterized definition auto-calling: .[array] becomes array()
// when array is defined as array(t).
//
// The env should contain the schema's definitions as callable functions for
// parameterized defs, and as IR nodes for non-parameterized defs.
func ExpandDefBody(body *ir.Node, env map[string]any, opts *eval.EvalOptions) (*ir.Node, error) {
	return eval.ExpandIRWithOptions(body, env, opts)
}

// BuildDefEnv creates an environment map from schema definitions.
// Parameterized definitions are wrapped as variadic functions that call InstantiateDef.
// Non-parameterized definitions are stored as IR nodes.
func BuildDefEnv(s *Schema) map[string]any {
	if s == nil || s.Define == nil {
		return nil
	}

	env := make(map[string]any)

	for defName, defBody := range s.Define {
		baseName, params := ParseDefSignature(defName)

		if len(params) == 0 {
			// Non-parameterized: store the body directly
			env[baseName] = defBody
		} else {
			// Parameterized: wrap as a function that instantiates
			// Capture defBody and params in closure
			body := defBody
			paramNames := params
			env[baseName] = func(args ...any) any {
				// Convert args to IR nodes
				irArgs := make([]*ir.Node, len(args))
				for i, arg := range args {
					switch v := arg.(type) {
					case *ir.Node:
						irArgs[i] = v
					case string:
						irArgs[i] = ir.FromString(v)
					default:
						// For other types, try to convert
						irArgs[i] = ir.FromString("")
					}
				}

				// If called with no args, return the base definition (uninstantiated clone)
				if len(irArgs) == 0 {
					return body.Clone()
				}

				// Instantiate with provided args
				result, err := InstantiateDef(body, paramNames, irArgs)
				if err != nil {
					// Return nil on error - caller should handle
					return nil
				}
				return result
			}
		}
	}

	return env
}
