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

	// Navigate through the document following the path structure. Segment matching
	// canonicalizes quoting (see valueAtPath / patchMayAffect): SplitAll yields a
	// digit-first key as the quoted "9digitdid" but it is stored unquoted, so the
	// segment is reduced to its canonical field name and the key is matched in
	// either stored form. A verbatim compare drops every quoted key (~62% of %08x
	// decision ids are digit-first), which here would report the subtree absent.
	current := doc
	parts := splitKPath(kp)

	for _, part := range parts {
		if current == nil || current.Type != ir.ObjectType {
			return nil, fmt.Errorf("%w: expected object at path segment %q", ErrPathNotFound, part)
		}

		name, isField := kpath.SegmentFieldName(part)
		if !isField {
			return nil, fmt.Errorf("%w: non-field path segment %q", ErrPathNotFound, part)
		}

		// Find the field matching this part
		found := false
		for i, field := range current.Fields {
			if field.String == name || unquoteFieldKey(field.String) == name {
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
// It delegates to tony.FilterState so logd and docd (which applies the same
// projection over a composed read) stay identical.
func filterState(state *ir.Node, match *ir.Node) (*ir.Node, error) {
	return tony.FilterState(state, match)
}
