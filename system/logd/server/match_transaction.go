package server

import (
	"fmt"
	"net/http"

	"github.com/signadot/tony-format/tony/encode"
	"github.com/signadot/tony-format/tony/ir"
	"github.com/signadot/tony-format/tony/system/logd/api"
)

// handleMatchTransaction handles MATCH requests for transaction status.
func (s *Server) handleMatchTransaction(w http.ResponseWriter, r *http.Request, body *api.RequestBody) {
	// Extract transactionId from match
	if body.Match == nil || body.Match.Type == ir.NullType {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, "match must contain transactionId"))
		return
	}

	transactionID, err := extractTransactionID(body.Match)
	if err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, fmt.Sprintf("invalid transactionId in match: %v", err)))
		return
	}

	// Read transaction state
	state, err := s.storage.ReadTransactionState(transactionID)
	if err != nil {
		writeError(w, http.StatusBadRequest, api.NewError("transaction_not_found", fmt.Sprintf("transaction not found: %v", err)))
		return
	}

	// Build response with transaction data
	participantCountNode := &ir.Node{Type: ir.NumberType, Int64: intPtr(int64(state.ParticipantCount)), Number: fmt.Sprintf("%d", state.ParticipantCount)}
	participantsReceivedNode := &ir.Node{Type: ir.NumberType, Int64: intPtr(int64(state.ParticipantsReceived)), Number: fmt.Sprintf("%d", state.ParticipantsReceived)}

	transactionNode := ir.FromMap(map[string]*ir.Node{
		"transactionId":        &ir.Node{Type: ir.StringType, String: state.TransactionID},
		"status":               &ir.Node{Type: ir.StringType, String: state.Status},
		"participantCount":    participantCountNode,
		"participantsReceived": participantsReceivedNode,
		"createdAt":           &ir.Node{Type: ir.StringType, String: state.CreatedAt},
	})

	response := ir.FromMap(map[string]*ir.Node{
		"path": &ir.Node{Type: ir.StringType, String: "/api/transactions"},
		"match": ir.FromMap(map[string]*ir.Node{
			"transactionId": &ir.Node{Type: ir.StringType, String: transactionID},
		}),
		"patch": transactionNode,
	})

	// Write response
	w.Header().Set("Content-Type", "application/x-tony")
	w.WriteHeader(http.StatusOK)
	if err := encode.Encode(response, w); err != nil {
		panic(fmt.Sprintf("failed to encode response: %v", err))
	}
}
