package server

import (
	"errors"
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// Reading: a match, and the documents a read and a watch are answered from.
//
// A read runs OFF the request loop (see dispatch), so what is here may be running
// while later requests are handled.

// handleMatch handles match (read) requests.
func (s *Session) handleMatch(id *string, req *api.MatchRequest) {
	// Check if session using pending is still valid
	if errMsg := s.checkPendingValid(); errMsg != "" {
		s.sendError(id, api.ErrCodeMigrationAborted, errMsg)
		return
	}

	path := req.Path

	// Validate path
	if err := validateDataPath(path); err != nil {
		s.sendError(id, api.ErrCodeInvalidPath, err.Error())
		return
	}

	// Resolve the commit to read at: an explicit historical commit if the request
	// carries one, otherwise the current commit. A historical commit must fall in
	// [0, current]; a commit past current would silently read as current and a
	// negative one as empty, so reject both rather than return misleading state.
	current, err := s.storage.GetCurrentCommit()
	if err != nil {
		s.sendError(id, api.ErrCodeStorage, fmt.Sprintf("failed to get current commit: %v", err))
		return
	}
	commit := current
	if req.Commit != nil {
		commit = *req.Commit
		if commit < 0 || commit > current {
			s.sendError(id, api.ErrCodeCommitNotFound,
				fmt.Sprintf("commit %d out of range [0, %d]", commit, current))
			return
		}
	}

	// Read state (with session scope filtering)
	doc, err := s.readDocAt(path, commit)
	if err != nil {
		s.sendError(id, api.ErrCodeStorage, fmt.Sprintf("failed to read state: %v", err))
		return
	}

	// Extract value at path. A bad segment is the client's path being wrong, not
	// its data being missing: reporting it as not-found reads as "nothing there
	// yet" and invites a retry that can never succeed.
	state, err := extractPathValue(doc, path)
	if err != nil {
		var pe *PathError
		if errors.As(err, &pe) && pe.Kind == PathBadSegment {
			s.sendError(id, api.ErrCodeInvalidPath, err.Error())
			return
		}
		if errors.Is(err, ErrPathNotFound) {
			s.sendError(id, api.ErrCodeNotFound, err.Error())
			return
		}
		s.sendError(id, api.ErrCodeStorage, fmt.Sprintf("failed to extract path value: %v", err))
		return
	}

	// Apply match filter if provided
	if req.Data != nil && req.Data.Type != ir.NullType {
		filteredState, err := filterState(state, req.Data)
		if err != nil {
			s.sendError(id, api.ErrCodeMatch, fmt.Sprintf("failed to apply match filter: %v", err))
			return
		}
		state = filteredState
	}

	s.send(api.NewMatchResponse(id, commit, state))
}

// readDocAt reads the document a request or a watch needs to answer about path: the
// subtree when the store can read it that way, the whole document when it cannot.
//
// A path restricts a read to the subdocument, which is what the request means and,
// until ReadSubtreeRootedAt, not what it cost: the whole document was replayed and
// materialized so that the value at the path could be taken out of it. What comes
// back here has the same SHAPE as the wide read -- rooted, with the subtree under its
// own path -- so everything downstream is unchanged: the path extraction with its
// quoting rules and its not-found reporting, the filter, the diffing a watcher does.
//
// The store declines to narrow where narrowing would have to guess: an operator above
// the path, a scoped read, a path holding nothing. Then this is the read it always
// was (ap8ddvp2h12krd43gdn0).
func (s *Session) readDocAt(path string, commit int64) (*ir.Node, error) {
	// Ask the cheapest question first. When the index can say the path has never been
	// written, it answers with a document which resolves exactly as far as the path
	// has -- so the extraction below fails at the same segment, with the same kind, as
	// it would have on the whole document.
	//
	// It is asked BEFORE the narrow read, not after: a path with nothing at it cannot
	// narrow, so asking second meant paying a failed narrowing to be told what an index
	// lookup already knew. Staging measured those declined attempts at 785ms each, 43
	// of them, in front of an answer that costs nothing -- and counted each read twice
	// besides, once as wide-absent and once as narrow-absent
	// (ap8ddvp2h12krd43gdn0).
	if spine, ok := s.storage.AbsentSpineAt(path, s.scopeID()); ok {
		return spine, nil
	}
	doc, narrowed, err := s.storage.ReadSubtreeRootedAt(path, commit, s.scopeID())
	if err != nil {
		return nil, err
	}
	if narrowed {
		return doc, nil
	}
	return s.storage.ReadStateAt(path, commit, s.scopeID())
}

// fullDocAt reads the whole document at a commit, normalized so an empty store is
// ir.Null(). It is the seed for a stepped watch (see forwardEvents): the one O(history)
// read a watch pays, after which each commit costs one patch application instead.
func (s *Session) fullDocAt(commit int64) (*ir.Node, error) {
	if commit <= 0 {
		return ir.Null(), nil
	}
	doc, err := s.storage.ReadStateAt("", commit, s.scopeID())
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return ir.Null(), nil
	}
	return doc, nil
}

// scopedDocAt returns the scoped state document (root-rooted, the watched path's
// subtree) at the given commit, normalized so a nil/empty result becomes ir.Null()
// for diffing.
func (s *Session) scopedDocAt(path string, commit int64) (*ir.Node, error) {
	if commit == 0 {
		return ir.Null(), nil
	}
	doc, err := s.readDocAt(path, commit)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return ir.Null(), nil
	}
	// ReadStateAt returns a rooted SUPERSET: it collects ancestor-level index
	// segments and applies whole patch entries, so a read of "p.b" carries a
	// sibling "p.a" write done under the shared ancestor "p". Trim to the path's
	// own subtree (mirroring handleMatch and watch-init, which extract after
	// reading) so a scoped watcher does not see a sibling's write as a change.
	sub, err := extractPathValue(doc, path)
	if err != nil {
		return ir.Null(), nil // path absent in this commit's state
	}
	if sub == nil || sub.Type == ir.NullType {
		return ir.Null(), nil
	}
	return sub, nil
}
