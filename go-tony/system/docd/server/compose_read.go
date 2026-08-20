package server

import (
	"fmt"
	"sort"
	"time"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// matchReadTimeout bounds each source read in a composed match, so one stalled
// source cannot hang the client's read indefinitely.
const matchReadTimeout = 10 * time.Second

// maybeCoordinateMatch handles a client MATCH whose path is a strict ancestor of
// one or more mounts (nested mounts included): docd single-routes reads, so
// without composition such a read would go only to its base owner and be blind to
// every mounted subtree beneath it. The read is fanned across the base owner and
// each mount below, and the subtrees merged into one document.
//
// It returns true when it has taken ownership of the request (the merge runs in a
// background goroutine so the read loop keeps serving). A read with no mount
// beneath it — the common single-owner case — returns false to fall through to
// normal routing. Root and .meta reads are out of scope and single-routed.
func (s *ClientSession) maybeCoordinateMatch(req *logdapi.SessionRequest) bool {
	path := req.Match.Path
	if path == "" || isMetaPath(path) {
		return false
	}
	below := s.server.Mounts.MountsUnder(path)
	if len(below) == 0 {
		return false
	}
	go s.coordinateMatch(req, below)
	return true
}

// coordinateMatch reads the base owner of path plus every mount below it
// concurrently, then merges the results — deeper mounts overlaying shallower —
// into a single MatchResult rooted at path. A tombstoned mount anywhere in range
// fails the whole read with controller_unavailable, matching the write path's
// refusal to compose across a dead controller.
func (s *ClientSession) coordinateMatch(req *logdapi.SessionRequest, below []*MountEntry) {
	clientID := req.ID
	path := req.Match.Path

	owner, pFields, errResp := s.composeCheck(clientID, path, below)
	if errResp != nil {
		_ = s.writeToClient(errResp)
		return
	}
	// A historical read at a commit fans the SAME commit to every source, which is
	// coherent because there is one sequence: a mount commits through the backing
	// logd under the tx id docd allocates, all-or-nothing, so every mount advances
	// in it (see coordinatePatch, and Handler.Match on what a mount owes a commit).
	root, commit, err := s.composeReadTree(path, owner, below, pFields, req.Match.Commit)
	if err != nil {
		// The match failed. It used to report session_closed, which named a
		// condition that had not happened — the client's session is fine, and a
		// client that believes otherwise reconnects a healthy connection instead of
		// dealing with the read that actually broke.
		_ = s.writeToClient(logdapi.NewErrorResponse(clientID, logdapi.ErrCodeMatchFailed, err.Error()))
		return
	}

	// A match/trim pattern is applied to the assembled tree here rather than
	// decomposed across sources, so the projection is identical to logd's (and to a
	// single-routed match, where the pattern rides the forwarded request). Sources
	// are read in full; narrowing the fan-out by the pattern is a later refinement.
	if pattern := req.Match.Data; pattern != nil && pattern.Type != ir.NullType {
		filtered, ferr := tony.FilterState(root, pattern)
		if ferr != nil {
			_ = s.writeToClient(logdapi.NewErrorResponse(clientID, logdapi.ErrCodeMatch, ferr.Error()))
			return
		}
		root = filtered
	}
	_ = s.writeToClient(logdapi.NewMatchResponse(clientID, commit, root))
}

// composeCheck validates that path can be composed — parsing it and refusing if a
// tombstoned controller owns any subtree in range (matching the write path's
// refusal to compose across a dead controller). It returns the base owner (nil =
// logd) and path fields, or a ready-to-send error response.
func (s *ClientSession) composeCheck(clientID *string, path string, below []*MountEntry) (*MountEntry, []string, *logdapi.SessionResponse) {
	pFields, err := pathFields(path)
	if err != nil {
		return nil, nil, logdapi.NewErrorResponse(clientID, logdapi.ErrCodeInvalidPath, err.Error())
	}
	for _, m := range below {
		if !m.Live() {
			return nil, nil, logdapi.NewErrorResponse(clientID, logdapi.ErrCodeUnavailable,
				fmt.Sprintf("controller for %q is unavailable", m.Path))
		}
	}
	// The base owner is the deepest mount containing path, or logd (nil) when path
	// sits on a base/unmounted region.
	owner := s.server.Mounts.LookupPrefix(path)
	if owner != nil && !owner.Live() {
		return nil, nil, logdapi.NewErrorResponse(clientID, logdapi.ErrCodeUnavailable,
			fmt.Sprintf("controller for %q is unavailable", owner.Path))
	}
	return owner, pFields, nil
}

// composeReadTree reads the base owner of path plus every mount below it
// concurrently, then merges the results — deeper mounts overlaying shallower —
// into a single document rooted at path, returning it with the max commit across
// sources.
func (s *ClientSession) composeReadTree(path string, owner *MountEntry, below []*MountEntry, pFields []string, atCommit *int64) (*ir.Node, int64, error) {
	// readResult carries one source's subtree and where it sits relative to path
	// (nil fields = the base owner, rooted at path itself).
	type readResult struct {
		fields []string
		body   *ir.Node
		commit int64
		err    error
	}
	results := make(chan readResult, len(below)+1)

	go func() {
		body, commit, err := s.readFrom(owner, path, atCommit)
		results <- readResult{fields: nil, body: body, commit: commit, err: err}
	}()
	for _, m := range below {
		go func(m *MountEntry) {
			mf, ferr := pathFields(m.Path) // validated at registration
			if ferr != nil {
				results <- readResult{err: ferr}
				return
			}
			body, commit, err := s.readFrom(m, m.Path, atCommit)
			results <- readResult{fields: mf[len(pFields):], body: body, commit: commit, err: err}
		}(m)
	}

	collected := make([]readResult, 0, len(below)+1)
	var firstErr error
	var commit int64
	for i := 0; i < len(below)+1; i++ {
		r := <-results
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		if r.commit > commit {
			commit = r.commit
		}
		collected = append(collected, r)
	}
	if firstErr != nil {
		return nil, 0, firstErr
	}

	// Overlay shallow→deep so a nested mount replaces its slot within the enclosing
	// mount's (or base owner's) subtree.
	sort.SliceStable(collected, func(i, j int) bool {
		return len(collected[i].fields) < len(collected[j].fields)
	})
	var root *ir.Node
	for _, r := range collected {
		if len(r.fields) == 0 {
			root = r.body
			continue
		}
		root = setAtFields(root, r.fields, r.body)
	}
	return root, commit, nil
}

// readFrom reads the state at path from one source: a controller (entry non-nil)
// via a collected MATCH route, or logd (entry nil) over a short-lived connection.
// atCommit, when non-nil, reads historical state at that commit. It is the same commit
// for every source because there is one sequence for them to be in: a mount commits
// through the backing logd under a docd-allocated tx id, all-or-nothing.
func (s *ClientSession) readFrom(entry *MountEntry, path string, atCommit *int64) (*ir.Node, int64, error) {
	if entry == nil {
		return readLogdMatch(s.logdAddr, path, s.clientScope, atCommit, matchReadTimeout)
	}
	ch, done := entry.Session.RouteCollect(&logdapi.SessionRequest{
		Scope: s.clientScope,
		Match: &logdapi.MatchRequest{PathData: logdapi.PathData{Path: path}, Commit: atCommit},
	})
	defer done()
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, 0, fmt.Errorf("controller %q: %s", entry.Path, resp.Error.Message)
		}
		if resp.Result == nil || resp.Result.Match == nil {
			return nil, 0, fmt.Errorf("controller %q returned no match result", entry.Path)
		}
		return resp.Result.Match.Body, resp.Result.Match.Commit, nil
	case <-time.After(matchReadTimeout):
		return nil, 0, fmt.Errorf("controller %q match timed out", entry.Path)
	}
}

// setAtFields returns a node equal to root but with value placed at the given
// field path, creating intermediate objects and replacing whatever subtree
// occupied that path. A nil or non-object node encountered along the way is
// replaced by a fresh object. Existing field order is preserved; a new field is
// appended.
func setAtFields(root *ir.Node, fields []string, value *ir.Node) *ir.Node {
	if len(fields) == 0 {
		return value
	}
	key := fields[0]

	var existing *ir.Node
	isObj := root != nil && root.Type == ir.ObjectType
	if isObj {
		existing = ir.Get(root, key)
	}
	child := setAtFields(existing, fields[1:], value)

	var kvs []ir.KeyVal
	replaced := false
	if isObj {
		for i := range root.Fields {
			fk := root.Fields[i].String
			if fk == key {
				kvs = append(kvs, ir.KeyVal{Key: ir.FromString(key), Val: child})
				replaced = true
			} else {
				kvs = append(kvs, ir.KeyVal{Key: ir.FromString(fk), Val: root.Values[i]})
			}
		}
	}
	if !replaced {
		kvs = append(kvs, ir.KeyVal{Key: ir.FromString(key), Val: child})
	}
	return ir.FromKeyVals(kvs)
}
