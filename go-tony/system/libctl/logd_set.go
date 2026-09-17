package libctl

import (
	"context"
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// wildPath says the path names a set rather than one node, which is what decides
// whether an answer carrying neither path nor id is the one node asked about or one
// anonymous body of many.
func wildPath(path string) bool {
	kp, err := kpath.Parse(path)
	if err != nil {
		return false
	}
	for x := kp; x != nil; x = x.Next {
		if x.Wild() {
			return true
		}
	}
	return false
}

// setChanDepth is how many members of a set the reader buffers ahead of the caller.
// It is back-pressure, not a limit: a caller slower than the wire slows the read pump
// once the buffer fills, and no member is lost.
const setChanDepth = 64

// SetMember is one node of the set a wildcard path names: where it is, the name it
// lives under, what kind of node it is, and what is there -- whichever the read asked
// for. The path is the store's own spelling, so it is a path this session can read,
// patch or watch on its own.
//
// IterType is one of the api.Iter* names, and api.IterWildcard of it, appended to Path,
// is the set of this node's children: what to list next, and how.
type SetMember struct {
	Path     string
	ID       string
	IterType string
	Node     *ir.Node
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
	return s.MatchEachReturning(ctx, path, pattern, "", fn)
}

// MatchEachReturning is MatchEach with the retspec the request carries
// (api.MatchRequest.Return): "" for the server's default -- a set answers paths and
// bodies -- or a comma-separated list of api.Return* names. `api.ReturnPath + "," +
// api.ReturnIterType` is what a client walking the store asks: where each member is, and
// how to list under it.
//
// A path naming one node is answered once, as MatchEach answers it.
func (s *LogdSession) MatchEachReturning(ctx context.Context, path string, pattern *ir.Node, ret string, fn func(SetMember) error) (int64, error) {
	return s.MatchEachQuery(ctx, SetQuery{Path: path, Pattern: pattern, Return: ret}, fn)
}

// SetQuery is everything a set read asks: the path, the pattern each member is matched
// against, the retspec, and a depth bounding every `..` in the path (nil for unbounded;
// api.MatchRequest.Depth says what a depth means and where it is refused).
type SetQuery struct {
	Path    string
	Pattern *ir.Node
	Return  string
	Depth   *int
}

// MatchEachQuery is MatchEach over a SetQuery, which is how a depth is asked.
func (s *LogdSession) MatchEachQuery(ctx context.Context, q SetQuery, fn func(SetMember) error) (int64, error) {
	commit := int64(0)
	cursor := ""
	for {
		req := &api.MatchRequest{
			PathData: api.PathData{Path: q.Path, Data: q.Pattern},
			Cursor:   cursor,
			Return:   q.Return,
			Depth:    q.Depth,
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
	commit, err := s.MatchEachReturning(ctx, path, pattern, api.ReturnPath, func(m SetMember) error {
		out = append(out, m.Path)
		return nil
	})
	return out, commit, err
}

// MatchIDs is MatchPaths answering the name each member lives under, as the segment a
// client writes for it -- a1, [0], {7}, and (id=r1) for an element of a keyed array --
// rather than its whole path. The prefix is the caller's own, so this is the listing
// without it repeated on every member, and the prefix and an id together are a path.
func (s *LogdSession) MatchIDs(ctx context.Context, path string, pattern *ir.Node) ([]string, int64, error) {
	var out []string
	commit, err := s.MatchEachReturning(ctx, path, pattern, api.ReturnID, func(m SetMember) error {
		out = append(out, m.ID)
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
			member := SetMember{Path: m.Path, ID: m.ID, IterType: m.IterType, Node: m.Body}
			// Whether this is one node or a member of a set is the PATH's question, not
			// the answer's: a path naming one node is answered once, with its path or id
			// if the retspec asked for them, and there is no marker to wait for. Read off
			// the answer's shape, `return: path` at such a path waited for one.
			single := !wildPath(match.Path)
			if single && member.Path == "" {
				// The answer is about the path the caller sent. A member with neither path
				// nor id under a WILDCARD path is an anonymous body -- what a cumulative
				// read asks for -- and keeps its empty path rather than claiming to live
				// at the wildcard.
				member.Path = match.Path
			}
			if err := fn(member); err != nil {
				return "", m.Commit, err
			}
			if single {
				return "", m.Commit, nil
			}
		case <-ctx.Done():
			return "", 0, ctx.Err()
		case <-s.done:
			return "", 0, fmt.Errorf("session closed")
		}
	}
}
