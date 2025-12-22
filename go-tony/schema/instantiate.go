package schema

import (
	"fmt"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
)

// ParseDefSignature parses a definition name like "array(t)" or "nullable(t)"
// and returns the base name and parameter names.
// For "array(t)" returns ("array", ["t"])
// For "nullable" returns ("nullable", nil)
func ParseDefSignature(defName string) (string, []string) {
	// Use ir.TagArgs to parse - it handles parentheses
	// Prepend ! to make it look like a tag
	head, args, _ := ir.TagArgs("!" + defName)

	// Remove the ! prefix we added
	head = head[1:]

	return head, args
}

// InstantiateDef creates a new IR node by substituting parameters in a definition body
// with the provided arguments.
//
// Given a definition like:
//
//	array(t): !and
//	- .[array]
//	- !all.t null
//
// And calling InstantiateDef(body, ["t"], [ir.FromString("int")]):
//   - Tags containing the param name get substituted: !all.t -> !all.int
//   - String values matching the param name get replaced with the argument node
//
// The argument nodes are expected to represent simple tokens (strings like "int",
// "string", or potentially paths like "name.id").
func InstantiateDef(body *ir.Node, params []string, args []*ir.Node) (*ir.Node, error) {
	if len(params) != len(args) {
		return nil, fmt.Errorf("parameter count mismatch: got %d params, %d args", len(params), len(args))
	}

	if len(params) == 0 {
		return body.Clone(), nil
	}

	// Build param -> argString map for tag substitution
	paramMap := make(map[string]string, len(params))
	paramNodeMap := make(map[string]*ir.Node, len(params))
	for i, param := range params {
		arg := args[i]
		paramNodeMap[param] = arg
		// Extract string representation for tag substitution
		paramMap[param] = argToTagString(arg)
	}

	// Clone and walk the body, substituting params
	result := body.Clone()
	if err := substituteParams(result, paramMap, paramNodeMap); err != nil {
		return nil, err
	}

	return result, nil
}

// argToTagString extracts a string suitable for tag substitution from an IR node
func argToTagString(arg *ir.Node) string {
	switch arg.Type {
	case ir.StringType:
		return arg.String
	case ir.NumberType:
		if arg.Int64 != nil {
			return fmt.Sprintf("%d", *arg.Int64)
		}
		if arg.Float64 != nil {
			return fmt.Sprintf("%g", *arg.Float64)
		}
	case ir.BoolType:
		if arg.Bool {
			return "true"
		}
		return "false"
	}
	// For complex types, use empty string (shouldn't happen for schema args)
	return ""
}

// substituteParams walks an IR node tree and substitutes parameter references.
// Scoping rule: never substitute inside .[...] expressions (def references).
// With the .[def](.[arg]) syntax, params only appear in tags, not inside .[...].
func substituteParams(node *ir.Node, paramMap map[string]string, paramNodeMap map[string]*ir.Node) error {
	// Substitute in tag if present
	if node.Tag != "" {
		node.Tag = substituteInTag(node.Tag, paramMap)
	}

	// Substitute string values that match a parameter name,
	// but skip .[...] expressions (def references in expr-lang)
	if node.Type == ir.StringType && !isDefRef(node.String) {
		if replacement, ok := paramNodeMap[node.String]; ok {
			// Replace this node's content with the argument, preserving the original tag
			originalTag := node.Tag
			replaceNodeContent(node, replacement)
			if originalTag != "" && node.Tag == "" {
				node.Tag = originalTag
			}
		}
	}

	// Recurse into children
	for _, child := range node.Values {
		if err := substituteParams(child, paramMap, paramNodeMap); err != nil {
			return err
		}
	}
	for _, field := range node.Fields {
		if err := substituteParams(field, paramMap, paramNodeMap); err != nil {
			return err
		}
	}

	return nil
}

// substituteInTag uses TagTree to properly substitute parameter names in tags.
// Handles nested args like !array(array(t)) -> !array(array(int))
func substituteInTag(tag string, paramMap map[string]string) string {
	tree := ir.ParseTag(tag)
	if tree == nil {
		return tag
	}

	mapped := tree.Map(func(name string) string {
		if replacement, ok := paramMap[name]; ok {
			return replacement
		}
		return name
	})

	return mapped.String()
}

// isDefRef returns true if the string is a def reference expression: .[...]
// These are expr-lang expressions for looking up definitions and should not
// have parameter substitution applied inside them.
func isDefRef(s string) bool {
	return strings.HasPrefix(s, ".[") && strings.HasSuffix(s, "]")
}

// replaceNodeContent replaces dst's content with a clone of src, preserving parent refs
func replaceNodeContent(dst, src *ir.Node) {
	// Save parent references
	parent := dst.Parent
	parentIndex := dst.ParentIndex
	parentField := dst.ParentField

	// Clone src into dst
	src.CloneTo(dst)

	// Restore parent references
	dst.Parent = parent
	dst.ParentIndex = parentIndex
	dst.ParentField = parentField
}
