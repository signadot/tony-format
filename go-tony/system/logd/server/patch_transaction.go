package server

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// handlePatchTransaction handles PATCH requests for transaction operations (create/abort).
func (s *Server) handlePatchTransaction(w http.ResponseWriter, r *http.Request, body *api.Body) {
	// Check if this is a create (match is null) or abort (match has transactionId)
	if body.Match == nil || body.Match.Type == ir.NullType {
		// Create transaction
		s.handleCreateTransaction(w, r, body)
		return
	}

	// Extract transactionId from match
	transactionID, err := extractTransactionID(body.Match)
	if err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, fmt.Sprintf("invalid transactionId in match: %v", err)))
		return
	}

	// Check if this is an abort (patch is !delete)
	if body.Patch != nil && body.Patch.Tag == "!delete" {
		s.handleAbortTransaction(w, r, body, transactionID)
		return
	}

	// Other operations not yet implemented
	writeError(w, http.StatusNotImplemented, api.NewError("not_implemented", "only create and abort transactions are supported"))
}

// handleCreateTransaction handles transaction creation.
func (s *Server) handleCreateTransaction(w http.ResponseWriter, r *http.Request, body *api.Body) {
	// Extract participantCount from patch
	if body.Patch == nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidDiff, "patch is required"))
		return
	}

	participantCount, err := extractParticipantCount(body.Patch)
	if err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidDiff, fmt.Sprintf("invalid participantCount: %v", err)))
		return
	}

	if participantCount < 1 {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidDiff, "participantCount must be at least 1"))
		return
	}

	// Get next transaction sequence number
	txSeq, err := s.Config.Storage.NextTxSeq()
	if err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to get tx seq: %v", err)))
		return
	}

	// Generate transaction ID: tx-{txSeq}-{participantCount}
	transactionID := fmt.Sprintf("tx-%d-%d", txSeq, participantCount)

	// Create transaction state
	state := storage.NewTransactionState(transactionID, participantCount)

	// Write transaction state
	if err := s.Config.Storage.WriteTransactionState(state); err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to write transaction state: %v", err)))
		return
	}

	// Build response with !key(id) structure
	insertNode := ir.FromMap(map[string]*ir.Node{
		"transactionId":    &ir.Node{Type: ir.StringType, String: transactionID},
		"participantCount": &ir.Node{Type: ir.NumberType, Int64: intPtr(int64(participantCount)), Number: fmt.Sprintf("%d", participantCount)},
		"status":           &ir.Node{Type: ir.StringType, String: "pending"},
	})
	insertNode.Tag = "!insert"

	// Build array with !key(id) tag
	patchNode := ir.FromSlice([]*ir.Node{insertNode})
	patchNode.Tag = "!key(id)"

	response := ir.FromMap(map[string]*ir.Node{
		"path":  &ir.Node{Type: ir.StringType, String: "/api/transactions"},
		"match": ir.Null(),
		"patch": patchNode,
	})

	// Write response
	w.Header().Set("Content-Type", "application/x-tony")
	w.WriteHeader(http.StatusOK)
	if err := encode.Encode(response, w); err != nil {
		panic(fmt.Sprintf("failed to encode response: %v", err))
	}
}

// handleAbortTransaction handles transaction abortion.
func (s *Server) handleAbortTransaction(w http.ResponseWriter, r *http.Request, body *api.Body, transactionID string) {
	// Acquire waiter and ensure it's released when function exits
	waiter := s.acquireWaiter(transactionID)
	defer s.releaseWaiter(transactionID)

	// Read transaction state
	state, err := s.Config.Storage.ReadTransactionState(transactionID)
	if err != nil {
		writeError(w, http.StatusBadRequest, api.NewError("transaction_not_found", fmt.Sprintf("transaction not found: %v", err)))
		return
	}

	// Check if transaction can be aborted
	if state.Status != "pending" {
		writeError(w, http.StatusBadRequest, api.NewError("invalid_transaction_state", fmt.Sprintf("transaction %s is %s, cannot abort", transactionID, state.Status)))
		return
	}

	// Clean up pending files
	for _, diff := range state.Diffs {
		// Extract txSeq from pending filename
		filename := filepath.Base(diff.DiffFile)
		if strings.HasSuffix(filename, ".pending") {
			txSeqStr := strings.TrimSuffix(filename, ".pending")
			if txSeq, err := strconv.ParseInt(txSeqStr, 10, 64); err == nil {
				// Delete the pending file
				if err := s.Config.Storage.DeletePendingDiff(diff.Path, txSeq); err != nil {
					// Log error but continue with abort
					// TODO: Add proper logging
				}
			}
		}
	}

	// Update transaction state to aborted
	state.Status = "aborted"
	participantsDiscarded := state.ParticipantsReceived

	if err := s.Config.Storage.WriteTransactionState(state); err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to update transaction state: %v", err)))
		return
	}

	// Notify any waiting writes that the transaction was aborted
	waiter.SetResult(&transactionResult{
		committed: false,
		err:       fmt.Errorf("transaction aborted"),
	})

	// Build response with !key(id) structure
	// Status replace operation using FromMap
	statusReplace := ir.FromMap(map[string]*ir.Node{
		"from": &ir.Node{Type: ir.StringType, String: "pending"},
		"to":   &ir.Node{Type: ir.StringType, String: "aborted"},
	})
	statusReplace.Tag = "!replace"

	transactionNode := ir.FromMap(map[string]*ir.Node{
		"transactionId":         &ir.Node{Type: ir.StringType, String: transactionID},
		"status":                statusReplace,
		"participantsDiscarded": &ir.Node{Type: ir.NumberType, Int64: intPtr(int64(participantsDiscarded)), Number: fmt.Sprintf("%d", participantsDiscarded), Tag: "!insert"},
	})

	// Build array with !key(id) tag
	patchNode := ir.FromSlice([]*ir.Node{transactionNode})
	patchNode.Tag = "!key(id)"

	response := ir.FromMap(map[string]*ir.Node{
		"path": &ir.Node{Type: ir.StringType, String: "/api/transactions"},
		"match": ir.FromMap(map[string]*ir.Node{
			"transactionId": &ir.Node{Type: ir.StringType, String: transactionID},
		}),
		"patch": patchNode,
	})

	// Write response
	w.Header().Set("Content-Type", "application/x-tony")
	w.WriteHeader(http.StatusOK)
	if err := encode.Encode(response, w); err != nil {
		panic(fmt.Sprintf("failed to encode response: %v", err))
	}
}

// extractTransactionID extracts the transactionId from a match node.
func extractTransactionID(match *ir.Node) (string, error) {
	if match == nil || match.Type != ir.ObjectType {
		return "", fmt.Errorf("match must be an object with transactionId field")
	}

	// Convert to map for easier field access
	matchMap := ir.ToMap(match)
	if txIDNode, ok := matchMap["transactionId"]; ok {
		if txIDNode.Type != ir.StringType {
			return "", fmt.Errorf("transactionId must be a string")
		}
		return txIDNode.String, nil
	}

	return "", fmt.Errorf("transactionId not found in match")
}

// extractParticipantCount extracts participantCount from a patch node.
func extractParticipantCount(patch *ir.Node) (int, error) {
	if patch == nil {
		return 0, fmt.Errorf("patch is nil")
	}

	// Handle !key(id) structure - tag can be "!key(id)" with arguments
	if (patch.Tag == "!key" || strings.HasPrefix(patch.Tag, "!key(")) && patch.Type == ir.ArrayType && len(patch.Values) > 0 {
		// Get first element (the !insert)
		insertNode := patch.Values[0]
		if insertNode.Tag == "!insert" && insertNode.Type == ir.ObjectType {
			// Use ir.Get to find participantCount
			participantCountNode := ir.Get(insertNode, "participantCount")
			if participantCountNode != nil {
				if participantCountNode.Type != ir.NumberType || participantCountNode.Int64 == nil {
					return 0, fmt.Errorf("participantCount must be a number")
				}
				return int(*participantCountNode.Int64), nil
			}
		}
	}

	return 0, fmt.Errorf("participantCount not found in patch")
}

// intPtr returns a pointer to the given int64.
func intPtr(i int64) *int64 {
	return &i
}
