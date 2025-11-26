package server

import (
	"fmt"
	"net/http"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// handleMatchData handles MATCH requests for data reads.
func (s *Server) handleMatchData(w http.ResponseWriter, r *http.Request, req *api.Match) {
	// Validate path
	if err := validateDataPath(req.Body.Path); err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, err.Error()))
		return
	}

	// Reconstruct state
	state, seq, err := s.reconstructState(req.Body.Path, req.Meta.SeqID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to reconstruct state: %v", err)))
		return
	}

	// req.Meta will be used for output, set the reconstructed state seqID
	resp := &api.Match{
		Meta: req.Meta,
	}
	resp.Meta.SeqID = &seq

	// Apply match filter if provided
	if req.Body.Match != nil && req.Body.Match.Type != ir.NullType {
		filteredState, err := s.filterState(state, req.Body.Match)
		if err != nil {
			writeError(w, http.StatusBadRequest, api.NewError("match_error", fmt.Sprintf("failed to apply match filter: %v", err)))
			return
		}
		state = filteredState
	}
	resp.Body.Patch = state
	resp.Body.Match = ir.Null()

	d, err := resp.ToTony()
	if err != nil {
		writeError(w, http.StatusBadRequest, api.NewError("match_error", fmt.Sprintf("failed to encode response: %v", err)))
		return
	}

	// Write response
	w.Header().Set("Content-Type", "application/x-tony")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(d); err != nil {
		// Error encoding response - header already written, can't send error
		// This is a programming error, should not happen
		panic(fmt.Sprintf("failed to encode response: %v", err))
	}
}

// filterState filters the state array to only include items that match the match criteria
// and trims the items to the match w.r.t. !trim and !notrim tags
func (s *Server) filterState(state *ir.Node, match *ir.Node) (*ir.Node, error) {
	// If state is not an array, just check if it matches
	if state.Type != ir.ArrayType {
		matches, err := tony.Match(state, match)
		if err != nil {
			return nil, err
		}
		if matches {
			return tony.Trim(match, state), nil
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
