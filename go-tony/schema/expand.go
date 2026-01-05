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
//
// When both parameterized and non-parameterized versions exist (e.g., array and array(t)),
// the function returns the non-parameterized version when called with no args,
// and instantiates the parameterized version when called with args.
func BuildDefEnv(s *Schema) map[string]any {
	if s == nil || s.Define == nil {
		return nil
	}

	// First pass: collect both parameterized and non-parameterized definitions
	nonParam := make(map[string]*ir.Node)
	paramDefs := make(map[string]struct {
		body   *ir.Node
		params []string
	})

	for defName, defBody := range s.Define {
		baseName, params := ParseDefSignature(defName)
		if len(params) == 0 {
			nonParam[baseName] = defBody
		} else {
			paramDefs[baseName] = struct {
				body   *ir.Node
				params []string
			}{body: defBody, params: params}
		}
	}

	env := make(map[string]any)

	// Process non-parameterized definitions first
	for baseName, defBody := range nonParam {
		// If there's also a parameterized version, create a function that handles both
		if pdef, hasParam := paramDefs[baseName]; hasParam {
			nonParamBody := defBody
			body := pdef.body
			paramNames := pdef.params

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
						irArgs[i] = ir.FromString("")
					}
				}

				// If called with no args, return non-parameterized version
				if len(irArgs) == 0 {
					return nonParamBody.Clone()
				}

				// Instantiate with provided args
				result, err := InstantiateDef(body, paramNames, irArgs)
				if err != nil {
					return nil
				}
				return result
			}
		} else {
			// Only non-parameterized version exists
			env[baseName] = defBody
		}
	}

	// Process parameterized definitions that don't have a non-parameterized version
	for baseName, pdef := range paramDefs {
		if _, hasNonParam := nonParam[baseName]; hasNonParam {
			continue // Already handled above
		}

		body := pdef.body
		paramNames := pdef.params

		env[baseName] = func(args ...any) any {
			irArgs := make([]*ir.Node, len(args))
			for i, arg := range args {
				switch v := arg.(type) {
				case *ir.Node:
					irArgs[i] = v
				case string:
					irArgs[i] = ir.FromString(v)
				default:
					irArgs[i] = ir.FromString("")
				}
			}

			// If called with no args, return uninstantiated clone
			if len(irArgs) == 0 {
				return body.Clone()
			}

			result, err := InstantiateDef(body, paramNames, irArgs)
			if err != nil {
				return nil
			}
			return result
		}
	}

	return env
}
