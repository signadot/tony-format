package server

import (
	"errors"
	"fmt"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// handleScratch answers a scratch operation: a write or a read of the store's scratch
// document, which has no history (storage/scratch.go). Exactly one of the two: a
// request saying both says two things, and is refused rather than guessed at.
func (s *Session) handleScratch(id *string, req *api.ScratchRequest) {
	switch {
	case req.Write != nil && req.Read != nil:
		s.sendError(id, api.ErrCodeInvalidMessage, "a scratch request names write or read, not both")
	case req.Write != nil:
		s.handleScratchWrite(id, req.Write)
	case req.Read != nil:
		s.handleScratchRead(id, req.Read)
	default:
		s.sendError(id, api.ErrCodeInvalidMessage, "a scratch request names write or read")
	}
}

// handleScratchWrite applies the patch and echoes it. A scratch path names one place,
// so it takes a write's rule (no wildcard, no `..`).
func (s *Session) handleScratchWrite(id *string, req *api.ScratchWriteRequest) {
	if err := validateDataPath(req.Path, roleWrite); err != nil {
		s.sendError(id, api.ErrCodeInvalidPath, err.Error())
		return
	}
	if req.Data == nil {
		s.sendError(id, api.ErrCodeInvalidDiff, "scratch data is required")
		return
	}
	if err := s.storage.ScratchWrite(req.Path, req.Data); err != nil {
		// What the client wrote is the problem, and the same problem next time: an
		// operation which executes, or a patch that does not apply to what is there.
		var apply *storage.ScratchApplyError
		if errors.As(err, &apply) {
			s.sendError(id, api.ErrCodeInvalidDiff, err.Error())
			return
		}
		s.sendError(id, api.ErrCodeStorage, fmt.Sprintf("scratch write failed: %v", err))
		return
	}
	s.send(api.NewScratchWriteResponse(id, req.Data))
}

// handleScratchRead answers the subtree, or an omitted data for absent. A read names
// one place too: scratch answers no sets.
func (s *Session) handleScratchRead(id *string, req *api.ScratchReadRequest) {
	if err := validateDataPath(req.Path, roleWrite); err != nil {
		s.sendError(id, api.ErrCodeInvalidPath, err.Error())
		return
	}
	data, err := s.storage.ScratchRead(req.Path)
	if err != nil {
		// Something is there, of a shape the path cannot step into: the document's
		// read says path_conflict, and so does this one.
		var pathErr *storage.ScratchPathError
		if errors.As(err, &pathErr) {
			s.sendError(id, api.ErrCodePathConflict, err.Error())
			return
		}
		s.sendError(id, api.ErrCodeStorage, fmt.Sprintf("scratch read failed: %v", err))
		return
	}
	s.send(api.NewScratchReadResponse(id, data))
}
