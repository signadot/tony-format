package server

import (
	"errors"
	"fmt"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// ErrPathNotFound is returned when a path does not exist in the document.
// This is distinct from a path that exists but has a null value.
var ErrPathNotFound = errors.New("path not found")

// extractPathValue navigates the document structure according to the kpath
// and returns the value at that path. The document structure mirrors the path.
// For example, path "users" with doc {users: {id: "1"}} returns {id: "1"}.
// Empty path returns the doc as-is.
func extractPathValue(doc *ir.Node, kp string) (*ir.Node, error) {
	if doc == nil {
		return ir.Null(), nil
	}
	if kp == "" {
		return doc, nil
	}

	// Navigate through the document following the path structure
	current := doc
	parts := splitKPath(kp)

	for _, part := range parts {
		if current == nil || current.Type != ir.ObjectType {
			return nil, fmt.Errorf("%w: expected object at path segment %q", ErrPathNotFound, part)
		}

		// Find the field matching this part
		found := false
		for i, field := range current.Fields {
			if field.String == part {
				current = current.Values[i]
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: path segment %q not found", ErrPathNotFound, part)
		}
	}

	return current, nil
}

// splitKPath splits a simple kpath into its parts.
// For now, only handles simple dot-separated field paths like "users.posts".
// TODO: handle array indices and sparse indices.
func splitKPath(kp string) []string {
	return kpath.SplitAll(kp)
}

// filterState filters the state to match the given criteria and trims the result.
func filterState(state *ir.Node, match *ir.Node) (*ir.Node, error) {
	// If state is not an array, just check if it matches
	if state.Type != ir.ArrayType {
		matches, err := tony.Match(state, match)
		if err != nil {
			return nil, err
		}
		if matches {
			return tony.Trim(match, state), nil
		}
		// Return null if doesn't match
		return ir.Null(), nil
	}

	// Filter array items that match
	var filtered []*ir.Node
	for _, item := range state.Values {
		matches, err := tony.Match(item, match)
		if err != nil {
			return nil, fmt.Errorf("match error on item: %w", err)
		}
		if matches {
			filtered = append(filtered, tony.Trim(match, item))
		}
	}

	// Preserve the tag from original state (e.g., !key(id))
	result := ir.FromSlice(filtered)
	if state.Tag != "" {
		result = result.WithTag(state.Tag)
	}
	return result, nil
}
