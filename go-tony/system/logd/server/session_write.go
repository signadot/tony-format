package server

import (
	"errors"
	"fmt"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// Writing: a patch, and the transaction a patch may join.
//
// A plain patch runs ON the request loop, which is what keeps a client's own ordering.
// A patch JOINING a transaction does not: it waits for the other participants, and
// waiting on the loop makes that wait unsatisfiable (zh3bm3msh12kscpygnn0).

// handlePatch handles patch (write) requests.
// If TxID is provided, the patch joins an existing multi-participant transaction.
// If TxID is nil, a new single-participant transaction is created.
// If Migration is true, the patch is only indexed to pending (for migration transforms).
func (s *Session) handlePatch(id *string, req *api.PatchRequest) {
	path := req.Path

	// Check if session using pending is still valid
	if errMsg := s.checkPendingValid(); errMsg != "" {
		s.sendError(id, api.ErrCodeMigrationAborted, errMsg)
		return
	}

	// Validate path
	if err := validateDataPath(path); err != nil {
		s.sendError(id, api.ErrCodeInvalidPath, err.Error())
		return
	}

	// Validate patch data
	if req.Data == nil {
		s.sendError(id, api.ErrCodeInvalidDiff, "patch data is required")
		return
	}

	// Parse timeout if provided
	var timeout time.Duration
	if req.Timeout != nil {
		var err error
		timeout, err = time.ParseDuration(*req.Timeout)
		if err != nil {
			s.sendError(id, api.ErrCodeInvalidTx, fmt.Sprintf("invalid timeout %q: %v", *req.Timeout, err))
			return
		}
	}

	var txn tx.Tx
	var err error

	if req.TxID != nil {
		// Join existing transaction
		txn, err = s.storage.GetTx(*req.TxID)
		if err != nil {
			s.sendError(id, api.ErrCodeTxNotFound, fmt.Sprintf("transaction %d not found: %v", *req.TxID, err))
			return
		}
		// Validate scope matches - all participants must have the same scope
		if !scopesEqual(s.scopeID(), txn.Scope()) {
			s.sendError(id, api.ErrCodeTxScopeMismatch, fmt.Sprintf("session scope %q doesn't match transaction scope %q", scopeStr(s.scopeID()), scopeStr(txn.Scope())))
			return
		}
		s.log.Debug("joining transaction", "txId", *req.TxID)
	} else {
		// Create single-participant transaction with session scope
		txn, err = s.storage.NewTx(1, s.scopeID())
		if err != nil {
			s.sendError(id, api.ErrCodeStorage, fmt.Sprintf("failed to create transaction: %v", err))
			return
		}
	}

	// Create patcher and commit. Match, if set, is a compare-and-swap
	// precondition evaluated atomically at commit time.
	patcher, err := txn.NewPatcher(&api.Patch{
		Match:    req.Match,
		PathData: req.PathData,
	})
	if err != nil {
		// A path which names no array element is the client's mistake, and it is the
		// same mistake next time: reporting it as a storage_error (or, in a
		// transaction, as tx_full) tells the client to retry something that cannot
		// work, and hides which path was wrong.
		var noElem *tx.NoSuchElementError
		if errors.As(err, &noElem) {
			s.sendError(id, api.ErrCodeInvalidPath, err.Error())
			return
		}
		// An operation which executes is the patch's problem, not the path's, and it
		// is the same problem next time: the client is told what it wrote, not that
		// the store failed.
		var unsafeOp *tx.UnsafeOpError
		if errors.As(err, &unsafeOp) {
			s.sendError(id, api.ErrCodeInvalidDiff, err.Error())
			return
		}
		if req.TxID != nil {
			s.sendError(id, api.ErrCodeTxFull, fmt.Sprintf("failed to join transaction: %v", err))
		} else {
			s.sendError(id, api.ErrCodeStorage, fmt.Sprintf("failed to create patcher: %v", err))
		}
		return
	}

	// Commit with optional per-participant timeout
	var result *tx.Result
	if timeout > 0 {
		resultCh := make(chan *tx.Result, 1)
		go func() {
			resultCh <- patcher.Commit()
		}()
		select {
		case result = <-resultCh:
			// Commit completed
		case <-time.After(timeout):
			s.sendError(id, api.ErrCodeTimeout, fmt.Sprintf("patch timed out after %v", timeout))
			return
		}
	} else {
		// No timeout - block until commit completes
		result = patcher.Commit()
	}

	if result.Error != nil {
		// The write was accepted and the array lost the element before it committed;
		// the store is healthy and the client's path is the thing that is wrong now.
		var noElem *tx.NoSuchElementError
		if errors.As(result.Error, &noElem) {
			s.sendError(id, api.ErrCodeInvalidPath, result.Error.Error())
			return
		}
		// The patch was well formed and does not apply to what is there now. The store
		// is healthy; storing it is what would have broken it.
		var noApply *api.DoesNotApplyError
		if errors.As(result.Error, &noApply) {
			s.sendError(id, api.ErrCodeInvalidDiff, result.Error.Error())
			return
		}
		s.sendError(id, api.ErrCodeStorage, fmt.Sprintf("failed to commit: %v", result.Error))
		return
	}
	if !result.Matched {
		s.sendError(id, api.ErrCodeMatchFailed, "transaction match condition failed")
		return
	}

	// Notify server for snapshot tracking
	if s.onCommit != nil {
		s.onCommit()
	}

	// Strip internal tags before sending to client
	tx.StripPatchRootTagRecursive(result.Data)
	s.send(api.NewPatchResponse(id, result.Commit, result.Data))
}

// handleNewTx handles newtx requests to create multi-participant transactions.
func (s *Session) handleNewTx(id *string, req *api.NewTxRequest) {
	// Check if session using pending is still valid
	if errMsg := s.checkPendingValid(); errMsg != "" {
		s.sendError(id, api.ErrCodeMigrationAborted, errMsg)
		return
	}

	if req.Participants < 1 {
		s.sendError(id, api.ErrCodeInvalidTx, "participants must be at least 1")
		return
	}

	tx, err := s.storage.NewTx(req.Participants, s.scopeID())
	if err != nil {
		s.sendError(id, api.ErrCodeStorage, fmt.Sprintf("failed to create transaction: %v", err))
		return
	}

	s.log.Debug("created transaction", "txId", tx.ID(), "participants", req.Participants)
	s.send(&api.SessionResponse{
		ID: id,
		Result: &api.SessionResult{
			NewTx: &api.NewTxResult{
				TxID: tx.ID(),
			},
		},
	})
}
