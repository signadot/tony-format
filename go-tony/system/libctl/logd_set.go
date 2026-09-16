package libctl

import (
	"context"
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// setChanDepth is how many members of a set the reader buffers ahead of the caller.
// It is back-pressure, not a limit: a caller slower than the wire slows the read pump
// once the buffer fills, and no member is lost.
const setChanDepth = 64

// SetMember is one node of the set a wildcard path names: where it is, and what is
// there. The path is the store's own spelling, so it is a path this session can read,
// patch or watch on its own.
type SetMember struct {
	Path string
	Node *ir.Node
}

// MatchEach reads the set at a wildcard path (.* [*] {*} (*) at any segment), calling
// fn once per member as it arrives, and answers the commit the set was read at.
//
// One node at a time is the point: a container of ten thousand is never assembled, and
// a caller that has seen enough stops by returning an error from fn, which MatchEach
// returns. Paging is followed here rather than by the caller -- the server bounds a
// page and says how to continue, and every page reads the commit the first one did, so
// the members fn sees are one snapshot however many pages they took.
//
// fn must not call back into this session: it runs on the read pump's side of the
// buffer, so a request made from inside it would wait for a pump that is waiting for
// it. Collect what is needed and act after.
//
// pattern, when non-nil, is matched and trimmed against each member on its own, and a
// member it rejects is not a member: fn does not see it.
func (s *LogdSession) MatchEach(ctx context.Context, path string, pattern *ir.Node, fn func(SetMember) error) (int64, error) {
	return s.matchEachReturning(ctx, path, pattern, "", fn)
}

// matchEachReturning is MatchEach with the retspec the request carries: "" for the
// server's default (a set answers paths and bodies), ReturnPaths for where the members
// are without what is in them.
func (s *LogdSession) matchEachReturning(ctx context.Context, path string, pattern *ir.Node, ret string, fn func(SetMember) error) (int64, error) {
	commit := int64(0)
	cursor := ""
	for {
		req := &api.MatchRequest{
			PathData: api.PathData{Path: path, Data: pattern},
			Cursor:   cursor,
			Return:   ret,
		}
		if commit != 0 {
			at := commit
			req.Commit = &at
		}
		next, at, err := s.matchSetPage(ctx, req, fn)
		if err != nil {
			return commit, err
		}
		commit = at
		if next == "" {
			return commit, nil
		}
		cursor = next
	}
}

// MatchSet is MatchEach gathered: every member of the set, and the commit it was read
// at. It is the convenience for a set a caller knows is small; a set of unknown size is
// what MatchEach is for.
func (s *LogdSession) MatchSet(ctx context.Context, path string, pattern *ir.Node) ([]SetMember, int64, error) {
	var out []SetMember
	commit, err := s.MatchEach(ctx, path, pattern, func(m SetMember) error {
		out = append(out, m)
		return nil
	})
	return out, commit, err
}

// MatchPaths answers WHERE the members of a set are, without what is in them, and the
// commit it read at. With no pattern the server reads no node to answer it -- the walk
// that finds the members already knows their names -- so "which ones are there?" over a
// large container costs the walk rather than a read per member.
//
// A pattern is still answered, and still costs the reads: the pattern has to see each
// node to select on it. What it saves then is the bodies on the wire.
func (s *LogdSession) MatchPaths(ctx context.Context, path string, pattern *ir.Node) ([]string, int64, error) {
	var out []string
	commit, err := s.matchEachReturning(ctx, path, pattern, api.ReturnPaths, func(m SetMember) error {
		out = append(out, m.Path)
		return nil
	})
	return out, commit, err
}

// matchSetPage sends one page's request and delivers its members, answering the cursor
// for the next page ("" when the set is finished) and the commit the set is read at.
func (s *LogdSession) matchSetPage(ctx context.Context, match *api.MatchRequest, fn func(SetMember) error) (string, int64, error) {
	id, ch, err := s.openStream(ctx, &api.SessionRequest{Match: match})
	if err != nil {
		return "", 0, err
	}
	defer s.closeStream(id)

	for {
		select {
		case resp, ok := <-ch:
			if !ok {
				return "", 0, s.connError()
			}
			if resp.Error != nil {
				return "", 0, fmt.Errorf("match error: %w", resp.Error)
			}
			if resp.Result == nil || resp.Result.Match == nil {
				return "", 0, fmt.Errorf("unexpected response: no match result")
			}
			m := resp.Result.Match
			if m.Done {
				return m.Cursor, m.Commit, nil
			}
			if m.Path == "" {
				// The path named one node after all, so there is no set to walk: the
				// answer is that node, and the read is over.
				if err := fn(SetMember{Path: match.Path, Node: m.Body}); err != nil {
					return "", m.Commit, err
				}
				return "", m.Commit, nil
			}
			if err := fn(SetMember{Path: m.Path, Node: m.Body}); err != nil {
				return "", m.Commit, err
			}
		case <-ctx.Done():
			return "", 0, ctx.Err()
		case <-s.done:
			return "", 0, fmt.Errorf("session closed")
		}
	}
}
