package server

import (
	"fmt"
	"net/http"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// handlePatchData handles PATCH requests for data writes.
func (s *Server) handlePatchData(w http.ResponseWriter, r *http.Request, req *api.Patch) {
	// Validate patch path
	if err := validateDataPath(req.Patch.Path); err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, err.Error()))
		return
	}

	// Validate patch is present
	if req.Patch.Data == nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidDiff, "patch data is required"))
		return
	}

	// Validate match path if provided
	if req.Match != nil && req.Match.Path != "" {
		if err := validateDataPath(req.Match.Path); err != nil {
			writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, fmt.Sprintf("match path invalid: %v", err)))
			return
		}
	}

	// For now, only support single-participant transactions
	// Multi-participant support can be added by using req.Meta.Tx
	if req.Meta.Tx != nil {
		// TODO: support multi-participant transactions via GetTx
		writeError(w, http.StatusNotImplemented, api.NewError("not_implemented", "multi-participant transactions not yet implemented"))
		return
	}

	// Create single-participant transaction
	tx, err := s.Config.Storage.NewTx(1, &req.Meta)
	if err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to create transaction: %v", err)))
		return
	}

	// Create patcher and commit
	patcher, err := tx.NewPatcher(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to create patcher: %v", err)))
		return
	}

	result := patcher.Commit()
	if result.Error != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to commit: %v", result.Error)))
		return
	}

	// Build response
	resp := &api.Patch{
		Meta: api.PatchMeta{
			EncodingOptions: req.Meta.EncodingOptions,
			Seq:             &result.Commit,
		},
		Match: req.Match,
		Patch: req.Patch,
	}

	d, err := resp.ToTony()
	if err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("internal_error", fmt.Sprintf("failed to encode response: %v", err)))
		return
	}

	w.Header().Set("Content-Type", "application/x-tony")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(d); err != nil {
		panic(fmt.Sprintf("failed to write response: %v", err))
	}
}
