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
	commit := int64(0)
	cursor := ""
	for {
		req := &api.MatchRequest{PathData: api.PathData{Path: path, Data: pattern}, Cursor: cursor}
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
