package mergeop

import (
	"errors"
	"fmt"
	"strings"

	"github.com/signadot/tony-format/tony/debug"
	"github.com/signadot/tony-format/tony/ir"
)

var letSym = &letSymbol{matchName: letName}

func Let() Symbol {
	return letSym
}

const (
	letName matchName = "let"
)

type letSymbol struct {
	matchName
}

func (s letSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("let op has no args, got %v", args)
	}
	if child.Type != ir.ObjectType {
		return nil, errors.New("let must be an object")
	}

	// Extract let: array
	letNode := ir.Get(child, "let")
	if letNode == nil {
		return nil, errors.New("let must have 'let' field")
	}
	if letNode.Type != ir.ArrayType {
		return nil, errors.New("let field must be an array")
	}

	// Extract in: node
	inNode := ir.Get(child, "in")
	if inNode == nil {
		return nil, errors.New("let must have 'in' field")
	}

	// Parse bindings from let array
	bindings := make(map[string]*ir.Node)
	for _, bindingItem := range letNode.Values {
		if bindingItem.Type != ir.ObjectType {
			return nil, errors.New("let binding must be an object")
		}
		// Each binding is an object with a single key-value pair
		if len(bindingItem.Fields) != 1 {
			return nil, errors.New("let binding must have exactly one field")
		}
		varName := bindingItem.Fields[0].String
		varValue := bindingItem.Values[0]
		bindings[varName] = varValue
	}

	op := &letOp{
		matchOp:  matchOp{op: op{name: s.matchName, child: child}},
		bindings: bindings,
		in:       inNode,
	}
	return op, nil
}

type letOp struct {
	matchOp
	bindings map[string]*ir.Node
	in       *ir.Node
}

func (l letOp) Match(doc *ir.Node, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("let op match on %s\n", doc.Path())
	}

	// Substitute variables in the 'in' node
	substitutedIn := l.substituteVariables(l.in.Clone())
	if substitutedIn == nil {
		return false, fmt.Errorf("variable substitution returned nil")
	}

	// Match using the substituted 'in' node
	return f(doc, substitutedIn)
}

// substituteVariables replaces variable references (like .idMatch) with their bound values
func (l letOp) substituteVariables(node *ir.Node) *ir.Node {
	switch node.Type {
	case ir.StringType:
		// Check if string starts with a dot that could be a variable reference
		// Handle escaping: \. escapes to literal ., \\. escapes to literal \., etc.
		// Only check the first dot for variable references
		if len(node.String) == 0 {
			return node
		}

		// Find the first dot
		dotIndex := strings.Index(node.String, ".")
		if dotIndex < 0 {
			return node
		}

		// Count trailing backslashes before the dot
		var backslashCount int
		if dotIndex > 0 {
			prefix := node.String[:dotIndex]
			for i := len(prefix) - 1; i >= 0 && prefix[i] == '\\'; i-- {
				backslashCount++
			}
		}

		// Only treat as variable reference if dot is at start with no backslashes
		// Any backslashes before the dot mean it's escaped (literal)
		if dotIndex == 0 && backslashCount == 0 {
			// Variable reference - substitute
			varName := node.String[1:] // Remove leading dot
			if boundValue, ok := l.bindings[varName]; ok {
				// Replace with bound value (clone to avoid modifying original)
				return boundValue.Clone()
			}
			// Variable not found - return as-is (literal string starting with .)
			return node
		}

		// Dot has backslashes before it - it's escaped, unescape and return literal
		if backslashCount > 0 {
			// Remove one backslash before the dot
			unescaped := node.String[:dotIndex-1] + "." + node.String[dotIndex+1:]
			return &ir.Node{
				Type:   ir.StringType,
				String: unescaped,
				Tag:    node.Tag,
			}
		}

		// Dot is not at start - return as-is (not a variable reference)
		return node

	case ir.ObjectType:
		// Recursively substitute in object values
		newValues := make([]*ir.Node, len(node.Values))
		for i, val := range node.Values {
			newValues[i] = l.substituteVariables(val)
		}
		// Reconstruct object with same fields but substituted values
		result := &ir.Node{
			Type:   ir.ObjectType,
			Fields: node.Fields,
			Values: newValues,
			Tag:    node.Tag,
		}
		// Update parent pointers
		for i := range result.Values {
			result.Values[i].Parent = result
			result.Values[i].ParentIndex = i
			if i < len(result.Fields) {
				result.Values[i].ParentField = result.Fields[i].String
			}
		}
		return result

	case ir.ArrayType:
		// Recursively substitute in array values
		newValues := make([]*ir.Node, len(node.Values))
		for i, val := range node.Values {
			newValues[i] = l.substituteVariables(val)
		}
		result := ir.FromSlice(newValues)
		if node.Tag != "" {
			result = result.WithTag(node.Tag)
		}
		return result

	default:
		return node
	}
}
