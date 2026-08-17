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
//	- !all t
//
// And calling InstantiateDef(body, ["t"], [intDefBody]):
//   - String values matching the param name get replaced with the argument node,
//     and a tag the parameter wore composes over the tag the argument has:
//     !all t with t bound to int's body, !ir {int: ...}, gives !all.ir {int: ...}
//   - Tags naming the param get substituted too: !all.t -> !all.int, but only
//     when the argument is a token that can be spelled as a tag component
//
// An argument is either a token (a name like "int", a path like "name.id") or a
// whole definition body -- a match, which has no spelling as a tag component.
// Naming such a parameter in a tag is an error rather than a silent hole; see
// substituteInTag.
func InstantiateDef(body *ir.Node, params []string, args []*ir.Node) (*ir.Node, error) {
	if len(params) != len(args) {
		return nil, fmt.Errorf("parameter count mismatch: got %d params, %d args", len(params), len(args))
	}

	if len(params) == 0 {
		return body.Clone(), nil
	}

	// Build param -> argString map for tag substitution.  A param whose
	// argument has no tag spelling is absent from paramMap and present in
	// paramNodeMap, which is how substituteInTag tells the two apart.
	paramMap := make(map[string]string, len(params))
	paramNodeMap := make(map[string]*ir.Node, len(params))
	for i, param := range params {
		arg := args[i]
		paramNodeMap[param] = arg
		if s, ok := argToTagString(arg); ok {
			paramMap[param] = s
		}
	}

	// Clone and walk the body, substituting params
	result := body.Clone()
	if err := substituteParams(result, paramMap, paramNodeMap); err != nil {
		return nil, err
	}

	return result, nil
}

// argToTagString extracts a string suitable for tag substitution from an IR
// node, and reports whether the node has one at all.
func argToTagString(arg *ir.Node) (string, bool) {
	if arg.Tag != "" {
		// A tagged argument is a match, not a token: !and [...] names no type.
		return "", false
	}
	switch arg.Type {
	case ir.StringType:
		return arg.String, true
	case ir.NumberType:
		if arg.Int64 != nil {
			return fmt.Sprintf("%d", *arg.Int64), true
		}
		if arg.Float64 != nil {
			return fmt.Sprintf("%g", *arg.Float64), true
		}
	case ir.BoolType:
		if arg.Bool {
			return "true", true
		}
		return "false", true
	}
	return "", false
}

// substituteParams walks an IR node tree and substitutes parameter references.
// Scoping rule: never substitute inside .[...] expressions (def references).
// With the .[def](.[arg]) syntax, params only appear in tags, not inside .[...].
func substituteParams(node *ir.Node, paramMap map[string]string, paramNodeMap map[string]*ir.Node) error {
	// Substitute in tag if present
	if node.Tag != "" {
		tag, err := substituteInTag(node.Tag, paramMap, paramNodeMap)
		if err != nil {
			return err
		}
		node.Tag = tag
	}

	// Substitute string values that match a parameter name,
	// but skip .[...] expressions (def references in expr-lang)
	if node.Type == ir.StringType && !isDefRef(node.String) {
		if replacement, ok := paramNodeMap[node.String]; ok {
			// Replace this node's content with the argument.  What the
			// parameter wore composes over what the argument wears, rather
			// than being dropped when the argument has a tag of its own:
			// `!all t` with t bound to `!and [...]` is `!all.and [...]`, and
			// dropping the !all left the container matched against the
			// element type -- the same shape retagRef fixes for .[ref].
			originalTag := node.Tag
			replaceNodeContent(node, replacement)
			if originalTag != "" {
				node.Tag = ir.TagCompose(originalTag, nil, node.Tag)
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
//
// A parameter bound to a definition body has no spelling as a tag component,
// and saying so is the difference between a schema that checks and one that
// only appears to.  `array(t): !and [.[array], !all.t null]` substituted the
// empty string for t, and `!all.` names no operation, so an unknown component
// in a pattern is ignored and the element check was decoration: .[array(int)]
// accepted a list of anything.  The parameter belongs in a value position,
// where the body substitutes whole and !all composes over the tag it wears.
func substituteInTag(tag string, paramMap map[string]string, paramNodeMap map[string]*ir.Node) (string, error) {
	tree := ir.ParseTag(tag)
	if tree == nil {
		return tag, nil
	}

	var err error
	mapped := tree.Map(func(name string) string {
		if replacement, ok := paramMap[name]; ok {
			return replacement
		}
		if _, isParam := paramNodeMap[name]; isParam && err == nil {
			err = fmt.Errorf("parameter %q cannot be written in the tag %q: it is bound to a match, not a name -- put the parameter in a value position instead, the way array(t) writes `!all t`", name, tag)
		}
		return name
	})
	if err != nil {
		return "", err
	}

	return mapped.String(), nil
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
