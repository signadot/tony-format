package server

import (
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/stream"
	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A retain request (logdapi.RetainRequest) is a write over several containers, and
// across composition each container has an owner: logd for a base path, or the
// controller whose mount holds it. docd routes each rule to its owner, in one request
// per owner, and answers the client with the rules in the order they were sent, each
// saying who ran it (Owner), which mounts beneath its container it did not reach
// (Under), and what the owner said if it refused (Error).
//
// That is the whole of it. A rule is not composed across the mounts beneath its
// container -- a controller answers for its subtree, and one that does not implement
// retain says unsupported, which the result reports for that rule. Nothing runs where
// a rule's owner refuses it, and nothing is retried. The time is fixed here when the
// client gave none, so every owner measures against the same now.
//
// It runs off the request loop, as a composed read does: an owner may take a while,
// and the client's other requests should not wait on it.

// retainTimeout bounds each owner's answer, so one stalled controller cannot hang the
// client's request indefinitely. Generous: a pass over a large container makes several
// commits.
const retainTimeout = 60 * time.Second

// retainGroup is the rules one owner runs, with their positions in the client's request.
type retainGroup struct {
	owner *MountEntry // nil is logd
	idx   []int
	rules []*logdapi.RetainRule
}

// coordinateRetain routes a client's retain request to the owners of its rules'
// containers and answers with the composed result.
func (s *ClientSession) coordinateRetain(req *logdapi.SessionRequest) {
	clientID := req.ID
	in := req.Retain
	if len(in.What) == 0 {
		// logd's answer, given here so a request with nothing in it is not fanned out
		// to nobody and answered from silence.
		_ = s.writeToClient(logdapi.NewErrorResponse(clientID, logdapi.ErrCodeInvalidRetain,
			"what is empty: a retain request names at least one rule"))
		return
	}

	// One time for every owner.
	now := in.Now
	if now == "" {
		now = time.Now().UTC().Format(time.RFC3339)
	}
	author := in.Author
	if author == "" {
		author = s.clientAuthor
	}

	// Group the rules by the owner of their container. A rule whose path does not
	// parse has no container to route by; it goes to logd, which refuses it and says
	// why, as it would have had the client spoken to logd directly.
	var groups []*retainGroup
	byOwner := map[*MountEntry]*retainGroup{}
	results := make([]*logdapi.RetainRuleResult, len(in.What))
	for i, rule := range in.What {
		results[i] = &logdapi.RetainRuleResult{Path: ruleOrEmpty(rule)}
		if rule == nil {
			continue
		}
		container := containerOf(rule.Path)
		owner := s.server.Mounts.LookupPrefix(container)
		results[i].Under = mountPathsUnder(s.server.Mounts, container)
		if owner != nil && !owner.Live() {
			results[i].Owner = owner.Path
			results[i].Error = &logdapi.SessionError{Code: logdapi.ErrCodeUnavailable,
				Message: fmt.Sprintf("controller for %q is unavailable", owner.Path)}
			continue
		}
		g := byOwner[owner]
		if g == nil {
			g = &retainGroup{owner: owner}
			byOwner[owner] = g
			groups = append(groups, g)
		}
		g.idx = append(g.idx, i)
		g.rules = append(g.rules, rule)
	}

	// Ask every owner, concurrently.
	type answer struct {
		g    *retainGroup
		resp *logdapi.SessionResponse
	}
	answers := make(chan answer, len(groups))
	for _, g := range groups {
		out := &logdapi.RetainRequest{Now: now, Batch: in.Batch, Author: author, What: g.rules}
		go func(g *retainGroup) {
			answers <- answer{g, s.retainFrom(g.owner, out)}
		}(g)
	}

	// Compose: rules back in the client's order, each saying who ran it.
	out := &logdapi.RetainResult{Now: now}
	ran := 0
	for range groups {
		a := <-answers
		ownerName := "logd"
		if a.g.owner != nil {
			ownerName = a.g.owner.Path
		}
		switch {
		case a.resp.Error != nil:
			for _, i := range a.g.idx {
				results[i].Owner = ownerName
				results[i].Error = a.resp.Error
			}
		case a.resp.Result == nil || a.resp.Result.Retain == nil:
			for _, i := range a.g.idx {
				results[i].Owner = ownerName
				results[i].Error = &logdapi.SessionError{Code: logdapi.ErrCodeStorage,
					Message: fmt.Sprintf("%s answered a retain with no retain result", ownerName)}
			}
		default:
			ran++
			r := a.resp.Result.Retain
			out.Deleted += r.Deleted
			out.Commit = max(out.Commit, r.Commit)
			for k, i := range a.g.idx {
				if k < len(r.Rules) && r.Rules[k] != nil {
					rr := *r.Rules[k]
					rr.Under = results[i].Under
					results[i] = &rr
				}
				results[i].Owner = ownerName
			}
		}
	}

	// Nothing ran anywhere: that is an error, and the first owner's reason is the
	// request's. Something ran: a result, and each rule says what became of it.
	if ran == 0 {
		for _, r := range results {
			if r.Error != nil {
				_ = s.writeToClient(logdapi.NewErrorResponse(clientID, r.Error.Code,
					fmt.Sprintf("%s: %s", r.Owner, r.Error.Message)))
				return
			}
		}
	}
	out.Rules = results
	_ = s.writeToClient(&logdapi.SessionResponse{ID: clientID, Result: &logdapi.SessionResult{Retain: out}})
}

// retainFrom asks one owner to run a group of rules and answers what it said: a
// controller over its mount session, logd over a short-lived connection in the client's
// scope, since the client's own logd link answers straight to the client.
func (s *ClientSession) retainFrom(owner *MountEntry, req *logdapi.RetainRequest) *logdapi.SessionResponse {
	if owner == nil {
		resp, err := retainOnLogd(s.logdAddr, s.clientScope, req, retainTimeout)
		if err != nil {
			return logdapi.NewErrorResponse(nil, logdapi.ErrCodeStorage, err.Error())
		}
		return resp
	}
	ch, done := owner.Session.RouteCollect(&logdapi.SessionRequest{Scope: s.clientScope, Retain: req})
	defer done()
	select {
	case resp := <-ch:
		return resp
	case <-time.After(retainTimeout):
		return logdapi.NewErrorResponse(nil, logdapi.ErrCodeTimeout,
			fmt.Sprintf("controller %q did not answer the retain within %v", owner.Path, retainTimeout))
	}
}

// retainOnLogd runs a retain request on logd over a short-lived connection in scope,
// and answers logd's response. The author rides the request (RetainRequest.Author,
// resolved by the caller), so this connection's hello plays no part in it.
func retainOnLogd(logdAddr string, scope *string, req *logdapi.RetainRequest, timeout time.Duration) (*logdapi.SessionResponse, error) {
	conn, err := net.DialTimeout("tcp", logdAddr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to logd at %s: %w", logdAddr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	dec, err := stream.NewDecoder(conn, stream.WithBrackets())
	if err != nil {
		return nil, err
	}
	if err := writeSessionRequest(conn, &logdapi.SessionRequest{Hello: logdHello("docd-retain", scope)}); err != nil {
		return nil, fmt.Errorf("hello: %w", err)
	}
	if _, err := readSessionResponse(dec); err != nil {
		return nil, fmt.Errorf("hello response: %w", err)
	}
	if err := writeSessionRequest(conn, &logdapi.SessionRequest{Retain: req}); err != nil {
		return nil, err
	}
	return readSessionResponse(dec)
}

// containerOf is the container a rule's items are the children of: the rule's path
// without its last segment. A path that does not parse answers itself, so it routes
// as written and is refused where it lands.
func containerOf(path string) string {
	kp, err := kpath.Parse(path)
	if err != nil || kp == nil {
		return path
	}
	if parent := kp.Parent(); parent != nil {
		return parent.String()
	}
	return ""
}

// mountPathsUnder is the paths of the mounts strictly beneath a container, sorted: what
// a rule over that container does not reach.
func mountPathsUnder(reg *MountRegistry, container string) []string {
	var out []string
	for _, m := range reg.MountsUnder(container) {
		out = append(out, m.Path)
	}
	sort.Strings(out)
	return out
}

func ruleOrEmpty(r *logdapi.RetainRule) string {
	if r == nil {
		return ""
	}
	return r.Path
}
