package server

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/signadot/tony-format/tony/encode"
	"github.com/signadot/tony-format/tony/ir"
	"github.com/signadot/tony-format/tony/system/logd/api"
	"github.com/signadot/tony-format/tony/system/logd/storage"
)

// handlePatchData handles PATCH requests for data writes.
func (s *Server) handlePatchData(w http.ResponseWriter, r *http.Request, body *api.RequestBody) {
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

	// Validate patch is present
	if body.Patch == nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidDiff, "patch is required"))
		return
	}

	// Check if this is a transaction write (has tx-id in meta)
	var transactionID string
	if body.Meta != nil && body.Meta.Type == ir.ObjectType {
		for i, field := range body.Meta.Fields {
			if field.String == "tx-id" {
				if i >= len(body.Meta.Values) {
					writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidDiff, "tx-id value is missing"))
					return
				}
				value := body.Meta.Values[i]
				if value.Type != ir.StringType {
					writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidDiff, "tx-id must be a string"))
					return
				}
				transactionID = value.String
				break
			}
		}
	}

	// Handle transaction write
	if transactionID != "" {
		s.handlePatchDataWithTransaction(w, r, body, pathStr, transactionID)
		return
	}

	// Get current timestamp
	timestamp := time.Now().UTC().Format(time.RFC3339)

	// Atomically allocate sequence numbers and write the diff
	// This ensures files are written in the order sequence numbers are allocated,
	// preventing race conditions where a later sequence number is written before an earlier one
	commitCount, _, err := s.storage.WriteDiffAtomically(pathStr, timestamp, body.Patch, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to write diff: %v", err)))
		return
	}

	// Build response: return the diff with meta fields
	// Use FromMap to maintain parent pointers
	seqNode := &ir.Node{Type: ir.NumberType, Int64: &commitCount, Number: fmt.Sprintf("%d", commitCount)}
	timestampNode := &ir.Node{Type: ir.StringType, String: timestamp}
	metaNode := ir.FromMap(map[string]*ir.Node{
		"seq":       seqNode,
		"timestamp": timestampNode,
	})

	response := ir.FromMap(map[string]*ir.Node{
		"path":  &ir.Node{Type: ir.StringType, String: pathStr},
		"match": ir.Null(),
		"patch": body.Patch,
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

// handlePatchDataWithTransaction handles PATCH requests for data writes within a transaction.
// This function blocks until the transaction is either committed or aborted.
func (s *Server) handlePatchDataWithTransaction(w http.ResponseWriter, r *http.Request, body *api.RequestBody, pathStr, transactionID string) {
	// Read transaction state
	state, err := s.storage.ReadTransactionState(transactionID)
	if err != nil {
		writeError(w, http.StatusBadRequest, api.NewError("transaction_not_found", fmt.Sprintf("transaction not found: %v", err)))
		return
	}

	// Validate transaction is pending
	if state.Status != "pending" {
		writeError(w, http.StatusBadRequest, api.NewError("invalid_transaction_state", fmt.Sprintf("transaction %s is %s, cannot write", transactionID, state.Status)))
		return
	}

	// Check if we've already received all participants
	if state.ParticipantsReceived >= state.ParticipantCount {
		writeError(w, http.StatusBadRequest, api.NewError("transaction_full", fmt.Sprintf("transaction %s already has all %d participants", transactionID, state.ParticipantCount)))
		return
	}

	// Get current timestamp
	timestamp := time.Now().UTC().Format(time.RFC3339)

	// Write diff as pending file
	// Each write gets its own txSeq allocated atomically, but they're all part of the same transaction
	_, writeTxSeq, err := s.storage.WriteDiffAtomically(pathStr, timestamp, body.Patch, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to write pending diff: %v", err)))
		return
	}

	// Get filesystem path for the pending file
	fsPath := s.storage.PathToFilesystem(pathStr)
	pendingFilename := fmt.Sprintf("%d.pending", writeTxSeq)
	pendingFilePath := filepath.Join(fsPath, pendingFilename)

	// Get or create waiter for this transaction
	waiter := s.getOrCreateWaiter(transactionID)

	// Register this write with the waiter
	write := pendingWrite{
		w:         w,
		r:         r,
		body:      body,
		pathStr:   pathStr,
		patch:     body.Patch,
		timestamp: timestamp,
		txSeq:     writeTxSeq,
	}

	if err := waiter.RegisterWrite(write); err != nil {
		if err == ErrTransactionCompleted {
			writeError(w, http.StatusInternalServerError, api.NewError("internal_error", "transaction already completed"))
		} else {
			writeError(w, http.StatusBadRequest, api.NewError("transaction_aborted", err.Error()))
		}
		return
	}

	// Atomically update transaction state and check if we're the last participant
	isLastParticipant, err := waiter.UpdateState(transactionID, s.storage, func(currentState *storage.TransactionState) {
		currentState.ParticipantsReceived++
		currentState.Diffs = append(currentState.Diffs, storage.PendingDiff{
			Path:      pathStr,
			DiffFile:  pendingFilePath,
			WrittenAt: timestamp,
		})
	})

	if err != nil {
		waiter.SetResult(&transactionResult{
			committed: false,
			err:       fmt.Errorf("failed to update transaction state: %w", err),
		})
		s.removeWaiter(transactionID)
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to update transaction state: %v", err)))
		return
	}

	// If this is the last participant, commit the transaction
	var result *transactionResult
	if isLastParticipant {
		// Re-read state to get the final state with all diffs
		finalState, err := s.storage.ReadTransactionState(transactionID)
		if err != nil {
			waiter.SetResult(&transactionResult{
				committed: false,
				err:       fmt.Errorf("failed to read final transaction state: %w", err),
			})
			s.removeWaiter(transactionID)
			writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to read final transaction state: %v", err)))
			return
		}
		s.commitTransaction(transactionID, finalState, waiter)
		// After committing, get the result (commitTransaction sets it)
		result = waiter.GetResult()
	} else {
		// Wait for transaction to complete or abort
		result = waiter.WaitForCompletion()
	}

	if result == nil {
		// This shouldn't happen, but handle it gracefully
		writeError(w, http.StatusInternalServerError, api.NewError("internal_error", "transaction completed but no result available"))
		return
	}

	if result.err != nil {
		// Transaction aborted or error occurred
		writeError(w, http.StatusBadRequest, api.NewError("transaction_aborted", result.err.Error()))
		return
	}

	// Transaction committed successfully - send success response
	seqNode := &ir.Node{Type: ir.NumberType, Int64: &result.commitCount, Number: fmt.Sprintf("%d", result.commitCount)}
	timestampNode := &ir.Node{Type: ir.StringType, String: timestamp}
	metaNode := ir.FromMap(map[string]*ir.Node{
		"seq":       seqNode,
		"timestamp": timestampNode,
		"tx-id":     &ir.Node{Type: ir.StringType, String: transactionID},
	})

	response := ir.FromMap(map[string]*ir.Node{
		"path":  &ir.Node{Type: ir.StringType, String: pathStr},
		"match": ir.Null(),
		"patch": body.Patch,
		"meta":  metaNode,
	})

	w.Header().Set("Content-Type", "application/x-tony")
	w.WriteHeader(http.StatusOK)
	if err := encode.Encode(response, w); err != nil {
		panic(fmt.Sprintf("failed to encode response: %v", err))
	}
}

// commitTransaction commits a transaction and notifies all waiting writes.
func (s *Server) commitTransaction(transactionID string, state *storage.TransactionState, waiter *transactionWaiter) {
	// Commit the transaction
	commitCount, err := s.storage.NextCommitCount()
	if err != nil {
		waiter.SetResult(&transactionResult{
			committed: false,
			err:       fmt.Errorf("failed to get commit count: %w", err),
		})
		s.removeWaiter(transactionID)
		return
	}

	// Rename all pending files to .diff files
	pendingFileRefs := make([]storage.PendingFileRef, len(state.Diffs))
	for i, diff := range state.Diffs {
		// Extract txSeq from pending filename (format: {txSeq}.pending)
		filename := filepath.Base(diff.DiffFile)
		if !strings.HasSuffix(filename, ".pending") {
			waiter.SetResult(&transactionResult{
				committed: false,
				err:       fmt.Errorf("invalid pending filename: %s", filename),
			})
			s.removeWaiter(transactionID)
			return
		}
		txSeqStr := strings.TrimSuffix(filename, ".pending")
		diffTxSeq, err := strconv.ParseInt(txSeqStr, 10, 64)
		if err != nil {
			waiter.SetResult(&transactionResult{
				committed: false,
				err:       fmt.Errorf("failed to parse txSeq from filename: %w", err),
			})
			s.removeWaiter(transactionID)
			return
		}

		// Rename pending file to diff file
		if err := s.storage.RenamePendingToDiff(diff.Path, commitCount, diffTxSeq); err != nil {
			waiter.SetResult(&transactionResult{
				committed: false,
				err:       fmt.Errorf("failed to rename pending file: %w", err),
			})
			s.removeWaiter(transactionID)
			return
		}

		pendingFileRefs[i] = storage.PendingFileRef{
			VirtualPath: diff.Path,
			TxSeq:       diffTxSeq,
		}
	}

	// Write transaction log entry
	logEntry := storage.NewTransactionLogEntry(commitCount, transactionID, pendingFileRefs)
	if err := s.storage.AppendTransactionLog(logEntry); err != nil {
		waiter.SetResult(&transactionResult{
			committed: false,
			err:       fmt.Errorf("failed to write transaction log: %w", err),
		})
		s.removeWaiter(transactionID)
		return
	}

	// Update transaction state to committed
	state.Status = "committed"
	if err := s.storage.WriteTransactionState(state); err != nil {
		waiter.SetResult(&transactionResult{
			committed: false,
			err:       fmt.Errorf("failed to update transaction state: %w", err),
		})
		s.removeWaiter(transactionID)
		return
	}

	// Notify all waiting writes
	waiter.SetResult(&transactionResult{
		committed:   true,
		commitCount: commitCount,
		err:         nil,
	})
	s.removeWaiter(transactionID)
}

// extractTxSeqFromTransactionID extracts the txSeq from a transaction ID.
// Format: tx-{txSeq}-{participantCount}
func extractTxSeqFromTransactionID(transactionID string) (int64, error) {
	if !strings.HasPrefix(transactionID, "tx-") {
		return 0, fmt.Errorf("transaction ID must start with 'tx-'")
	}

	// Remove "tx-" prefix
	rest := transactionID[3:]

	// Split by "-" to get txSeq and participantCount
	parts := strings.Split(rest, "-")
	if len(parts) != 2 {
		return 0, fmt.Errorf("transaction ID format must be tx-{txSeq}-{participantCount}")
	}

	txSeq, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid txSeq in transaction ID: %w", err)
	}

	return txSeq, nil
}
