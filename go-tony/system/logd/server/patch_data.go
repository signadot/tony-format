package server

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// handlePatchData handles PATCH requests for data writes.
func (s *Server) handlePatchData(w http.ResponseWriter, r *http.Request, req *api.Patch) {
	body := &req.Body
	// Validate path
	if err := validateDataPath(body.Path); err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, err.Error()))
		return
	}

	// Validate patch is present
	if body.Patch == nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidDiff, "patch is required"))
		return
	}

	// Handle transaction write
	if req.Meta.Tx != nil {
		s.handlePatchDataWithTransaction(w, r, req)
		return
	}

	// Get current timestamp
	timestamp := time.Now().UTC().Format(time.RFC3339)

	// Atomically allocate sequence numbers and write the diff
	// This ensures files are written in the order sequence numbers are allocated,
	// preventing race conditions where a later sequence number is written before an earlier one
	commitCount, _, err := s.Config.Storage.WriteDiffAtomically(body.Path, timestamp, body.Patch, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to write diff: %v", err)))
		return
	}

	// Build response: return the diff with meta fields
	resp := &api.Patch{
		Meta: api.PatchMeta{
			Tx:              req.Meta.Tx,
			EncodingOptions: req.Meta.EncodingOptions,
			MaxDuration:     req.Meta.MaxDuration,
			Seq:             &commitCount,
			When:            timestamp,
		},
		Body: api.Body{
			Path:  body.Path,
			Patch: body.Patch,
		},
	}
	d, err := resp.ToTony()
	if err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("internal_error", fmt.Sprintf("error encoding response: %v", err)))
		return
	}

	// Write response
	w.Header().Set("Content-Type", "application/x-tony")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(d)
	if err != nil {
		// Error encoding response - header already written, can't send error
		// This is a programming error, should not happen
		panic(fmt.Sprintf("failed to encode response: %v", err))
	}
}

// handlePatchDataWithTransaction handles PATCH requests for data writes within a transaction.
// This function blocks until the transaction is either committed or aborted.
func (s *Server) handlePatchDataWithTransaction(w http.ResponseWriter, r *http.Request, req *api.Patch) { //body *api.Body, pathStr, transactionID string) {
	// Acquire waiter and ensure it's released when function exits
	body := &req.Body
	path := body.Path
	txID := *req.Meta.Tx
	waiter := s.acquireWaiter(txID)
	defer s.releaseWaiter(txID)

	// Read transaction state
	state, err := s.Config.Storage.ReadTransactionState(txID)
	if err != nil {
		writeError(w, http.StatusBadRequest, api.NewError("transaction_not_found", fmt.Sprintf("transaction not found: %v", err)))
		return
	}

	// Validate transaction is pending
	if state.Status != "pending" {
		writeError(w, http.StatusBadRequest, api.NewError("invalid_transaction_state", fmt.Sprintf("transaction %s is %s, cannot write", txID, state.Status)))
		return
	}

	// Check if we've already received all participants
	if state.ParticipantsReceived >= state.ParticipantCount {
		writeError(w, http.StatusBadRequest, api.NewError("transaction_full", fmt.Sprintf("transaction %s already has all %d participants", txID, state.ParticipantCount)))
		return
	}

	// Get current timestamp
	timestamp := time.Now().UTC().Format(time.RFC3339)

	// Write diff as pending file
	// Each write gets its own txSeq allocated atomically, but they're all part of the same transaction
	_, writeTxSeq, err := s.Config.Storage.WriteDiffAtomically(path, timestamp, body.Patch, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to write pending diff: %v", err)))
		return
	}

	// Get filesystem path for the pending file
	fsPath := s.Config.Storage.FS.PathToFilesystem(path)
	pendingFilename := fmt.Sprintf("%d.pending", writeTxSeq)
	pendingFilePath := filepath.Join(fsPath, pendingFilename)

	// Register this write with the waiter
	write := pendingWrite{
		w:         w,
		r:         r,
		body:      body,
		pathStr:   path,
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
	isLastParticipant, err := waiter.UpdateState(txID, s.Config.Storage, func(currentState *storage.TransactionState) {
		currentState.ParticipantsReceived++
		currentState.Diffs = append(currentState.Diffs, storage.PendingDiff{
			Path:      path,
			DiffFile:  pendingFilePath,
			WrittenAt: timestamp,
		})
	})

	if err != nil {
		waiter.SetResult(&transactionResult{
			committed: false,
			err:       fmt.Errorf("failed to update transaction state: %w", err),
		})
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to update transaction state: %v", err)))
		return
	}

	// If this is the last participant, commit the transaction
	var result *transactionResult
	if isLastParticipant {
		// Re-read state to get the final state with all diffs
		finalState, err := s.Config.Storage.ReadTransactionState(txID)
		if err != nil {
			waiter.SetResult(&transactionResult{
				committed: false,
				err:       fmt.Errorf("failed to read final transaction state: %w", err),
			})
			writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to read final transaction state: %v", err)))
			return
		}
		s.commitTransaction(txID, finalState, waiter)
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
	resp := &api.Patch{
		Meta: api.PatchMeta{
			Seq:  &result.commitCount,
			When: timestamp,
			Tx:   &txID,
		},
		Body: api.Body{
			Path:  req.Body.Path,
			Patch: req.Body.Patch,
		},
	}
	seqNode := &ir.Node{Type: ir.NumberType, Int64: &result.commitCount, Number: fmt.Sprintf("%d", result.commitCount)}
	timestampNode := &ir.Node{Type: ir.StringType, String: timestamp}
	metaNode := ir.FromMap(map[string]*ir.Node{
		"seq":       seqNode,
		"timestamp": timestampNode,
		"tx-id":     &ir.Node{Type: ir.StringType, String: txID},
	})

	response := ir.FromMap(map[string]*ir.Node{
		"path":  ir.FromString(path),
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
	commitCount, err := s.Config.Storage.NextCommitCount()
	if err != nil {
		waiter.SetResult(&transactionResult{
			committed: false,
			err:       fmt.Errorf("failed to get commit count: %w", err),
		})
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
			return
		}
		txSeqStr := strings.TrimSuffix(filename, ".pending")
		diffTxSeq, err := strconv.ParseInt(txSeqStr, 10, 64)
		if err != nil {
			waiter.SetResult(&transactionResult{
				committed: false,
				err:       fmt.Errorf("failed to parse txSeq from filename: %w", err),
			})
			return
		}

		// Commit pending file (rename + update index)
		if err := s.Config.Storage.CommitPendingDiff(diff.Path, diffTxSeq, commitCount); err != nil {
			waiter.SetResult(&transactionResult{
				committed: false,
				err:       fmt.Errorf("failed to commit pending file: %w", err),
			})
			return
		}

		pendingFileRefs[i] = storage.PendingFileRef{
			VirtualPath: diff.Path,
			TxSeq:       diffTxSeq,
		}
	}

	// Write transaction log entry
	logEntry := storage.NewTransactionLogEntry(commitCount, transactionID, pendingFileRefs)
	if err := s.Config.Storage.AppendTransactionLog(logEntry); err != nil {
		waiter.SetResult(&transactionResult{
			committed: false,
			err:       fmt.Errorf("failed to write transaction log: %w", err),
		})
		return
	}

	// Update transaction state to committed
	state.Status = "committed"
	if err := s.Config.Storage.WriteTransactionState(state); err != nil {
		waiter.SetResult(&transactionResult{
			committed: false,
			err:       fmt.Errorf("failed to update transaction state: %w", err),
		})
		return
	}

	// Notify all waiting writes
	waiter.SetResult(&transactionResult{
		committed:   true,
		commitCount: commitCount,
		err:         nil,
	})
}
