package server

import (
	"fmt"
	"net/http"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// handleMatchData handles MATCH requests for data reads.
func (s *Server) handleMatchData(w http.ResponseWriter, r *http.Request, req *api.Match) {
	// Validate path (kpath format)
	if err := validateDataPath(req.Body.Path); err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, err.Error()))
		return
	}

	kp := req.Body.Path

	// Determine commit to read at
	var commit int64
	if req.Meta.SeqID != nil {
		commit = *req.Meta.SeqID
	} else {
		var err error
		commit, err = s.Spec.Storage.GetCurrentCommit()
		if err != nil {
			writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to get current commit: %v", err)))
			return
		}
	}

	// Read state at path (returns document with path as outer structure)
	doc, err := s.Spec.Storage.ReadStateAt(kp, commit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to read state: %v", err)))
		return
	}

	// Extract value at the path from the document
	state, err := extractPathValue(doc, kp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to extract path value: %v", err)))
		return
	}

	// Apply match filter if provided (Data field contains match criteria)
	if req.Body.Data != nil && req.Body.Data.Type != ir.NullType {
		filteredState, err := filterState(state, req.Body.Data)
		if err != nil {
			writeError(w, http.StatusBadRequest, api.NewError("match_error", fmt.Sprintf("failed to apply match filter: %v", err)))
			return
		}
		state = filteredState
	}

	// Build response
	resp := &api.Match{
		Meta: req.Meta,
		Body: api.Body{
			Path: req.Body.Path,
			Data: state,
		},
	}
	resp.Meta.SeqID = &commit

	d, err := resp.ToTony()
	if err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("match_error", fmt.Sprintf("failed to encode response: %v", err)))
		return
	}

	w.Header().Set("Content-Type", "application/x-tony")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(d); err != nil {
		panic(fmt.Sprintf("failed to write response: %v", err))
	}
}

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
			return nil, fmt.Errorf("expected object at path segment %q", part)
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
			return nil, fmt.Errorf("path segment %q not found in document", part)
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
