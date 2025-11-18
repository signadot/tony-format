package server

import (
	"fmt"
	"net/http"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// handleMatchData handles MATCH requests for data reads.
func (s *Server) handleMatchData(w http.ResponseWriter, r *http.Request, body *api.RequestBody) {
	// Extract path
	pathStr, err := extractPathString(body.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, fmt.Sprintf("invalid path: %v", err)))
		return
	}

	// Validate path
	if err := validateDataPath(pathStr); err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, err.Error()))
		return
	}

	// Extract seq from meta for time-travel (null = latest)
	var targetCommitCount *int64
	if body.Meta != nil && body.Meta.Type == ir.ObjectType {
		for i, field := range body.Meta.Fields {
			if field.String == "seq" {
				value := body.Meta.Values[i]
				if value != nil && value.Type == ir.NumberType && value.Int64 != nil {
					targetCommitCount = value.Int64
				}
				// If seq is null or not a number, use latest (targetCommitCount stays nil)
				break
			}
		}
	}

	// Reconstruct state using snapshot optimization if available
	state, seq, err := s.reconstructState(pathStr, targetCommitCount)
	if err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to reconstruct state: %v", err)))
		return
	}

	// Apply match filter if provided
	if body.Match != nil && body.Match.Type != ir.NullType {
		filteredState, err := s.filterState(state, body.Match)
		if err != nil {
			writeError(w, http.StatusBadRequest, api.NewError("match_error", fmt.Sprintf("failed to apply match filter: %v", err)))
			return
		}
		state = filteredState
	}

	// Build response: return the state as a diff from null
	seqNode := &ir.Node{Type: ir.NumberType, Int64: &seq, Number: fmt.Sprintf("%d", seq)}
	metaNode := ir.FromMap(map[string]*ir.Node{
		"seq": seqNode,
	})

	response := ir.FromMap(map[string]*ir.Node{
		"path":  &ir.Node{Type: ir.StringType, String: pathStr},
		"match": ir.Null(),
		"patch": state,
		"meta":  metaNode,
	})

	// Write response
	w.Header().Set("Content-Type", "application/x-tony")
	w.WriteHeader(http.StatusOK)
	if err := encode.Encode(response, w); err != nil {
		// Error encoding response - header already written, can't send error
		// This is a programming error, should not happen
		panic(fmt.Sprintf("failed to encode response: %v", err))
	}
}

// filterState filters the state array to only include items that match the match criteria.
func (s *Server) filterState(state *ir.Node, match *ir.Node) (*ir.Node, error) {
	// If state is not an array, just check if it matches
	if state.Type != ir.ArrayType {
		matches, err := tony.Match(state, match)
		if err != nil {
			return nil, err
		}
		if matches {
			return state, nil
		}
		// Return empty array if doesn't match
		return ir.FromSlice([]*ir.Node{}), nil
	}

	// Filter array items that match
	var filtered []*ir.Node
	for _, item := range state.Values {
		matches, err := tony.Match(item, match)
		if err != nil {
			return nil, fmt.Errorf("match error on item: %w", err)
		}
		if matches {
			filtered = append(filtered, item)
		}
	}

	// Preserve the tag from original state (e.g., !key(id))
	result := ir.FromSlice(filtered)
	if state.Tag != "" {
		result = result.WithTag(state.Tag)
	}
	return result, nil
}
