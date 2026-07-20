package server

import (
	"fmt"

	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// coordinateWatch registers a client watch as a reader with the mount
// coordinator, then routes it to its backend (a controller, or logd for a base
// path). Admission runs on its own goroutine so writer priority — a pending
// overlapping mount holding off new watches — does not stall the client's request
// loop and its other in-flight ops.
//
// A client is expected to unwatch a path only after its watch is confirmed, and
// the token is registered before the watch is routed (hence before any
// confirmation), so releaseWatchToken and close reliably find it. If the session
// is already closing when admission completes, the token is released rather than
// leaked past the session's lifetime.
func (s *ClientSession) coordinateWatch(req *logdapi.SessionRequest) {
	path := req.Watch.Path
	go func() {
		token, ok := s.server.coord.beginRead(path, func() { s.forceWatch(path) })
		if !ok {
			_ = s.writeToClient(logdapi.NewErrorResponse(req.ID, logdapi.ErrCodeInvalidPath,
				fmt.Sprintf("invalid watch path %q", path)))
			return
		}

		s.watchMu.Lock()
		if s.closing {
			s.watchMu.Unlock()
			s.server.coord.endRead(token)
			return
		}
		if old, exists := s.watchTokens[path]; exists { // re-watch replaces prior token
			s.server.coord.endRead(old)
		}
		s.watchTokens[path] = token
		s.watchMu.Unlock()

		switch dest, entry := s.routeFor(req); dest {
		case destController:
			entry.Session.RouteRequest(s, req)
		case destUnavailable:
			s.releaseWatchToken(path)
			_ = s.writeToClient(logdapi.NewErrorResponse(req.ID, logdapi.ErrCodeUnavailable,
				fmt.Sprintf("controller for %q is unavailable", entry.Path)))
		case destMeta:
			s.releaseWatchToken(path)
			_ = s.writeToClient(logdapi.NewErrorResponse(req.ID, logdapi.ErrCodeUnsupported,
				"cannot watch .meta"))
		default: // destLogd
			if err := s.writeToLogd(req); err != nil {
				s.releaseWatchToken(path)
				_ = s.writeToClient(logdapi.NewErrorResponse(req.ID, logdapi.ErrCodeSessionClosed, err.Error()))
			}
		}
	}()
}

// releaseWatchToken releases the coordinator reader token the client holds for
// path, if any. Safe to call when none is held (an unwatch of a base path docd
// did not track, or a watch already force-ended).
func (s *ClientSession) releaseWatchToken(path string) {
	s.watchMu.Lock()
	token, ok := s.watchTokens[path]
	if ok {
		delete(s.watchTokens, path)
	}
	s.watchMu.Unlock()
	if ok {
		s.server.coord.endRead(token)
	}
}

// forceWatch is the coordinator's force-teardown hook: a mount/unmount whose
// force_after elapsed is ending this watch so its membership can change. The
// coordinator has already dropped the reader, so this only drops the token and
// tears down the backend watch — the client's watch then simply stops delivering
// events, as it does today when a controller crashes. An explicit termination
// signal prompting the client to re-watch is a follow-up.
func (s *ClientSession) forceWatch(path string) {
	s.watchMu.Lock()
	_, ok := s.watchTokens[path]
	if ok {
		delete(s.watchTokens, path)
	}
	s.watchMu.Unlock()
	if !ok {
		return // already released (raced with unwatch/close)
	}

	unwatch := &logdapi.SessionRequest{Unwatch: &logdapi.UnwatchRequest{Path: path}}
	switch dest, entry := s.routeFor(unwatch); dest {
	case destController:
		entry.Session.RouteRequest(s, unwatch)
	case destLogd:
		_ = s.writeToLogd(unwatch)
	}
}

// releaseAllWatches marks the session closing and releases every coordinator
// reader token it still holds, so a departing client cannot wedge mounts waiting
// on watches that will never drain.
func (s *ClientSession) releaseAllWatches() {
	s.watchMu.Lock()
	s.closing = true
	tokens := make([]uint64, 0, len(s.watchTokens))
	for _, t := range s.watchTokens {
		tokens = append(tokens, t)
	}
	s.watchTokens = make(map[string]uint64)
	s.watchMu.Unlock()
	for _, t := range tokens {
		s.server.coord.endRead(t)
	}
}
