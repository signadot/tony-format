package server

import (
	"fmt"
	"sync"

	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// coordinateWatch registers a client watch as a reader with the mount coordinator
// (so a concurrent mount/unmount on an overlapping path drains or force-ends it),
// then serves it — composed across the base owner and mounts below when the watch
// path is a strict ancestor of one or more mounts, or single-routed otherwise.
//
// Admission runs on its own goroutine so writer priority — a pending overlapping
// mount holding off new watches — does not stall the client's request loop. A
// client is expected to unwatch a path only after its watch is confirmed, and the
// token is registered before the watch is served, so releaseWatchToken and close
// reliably find it. A session already closing when admission completes releases
// the token instead of leaking it.
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
		oldTok, hadOld := s.watchTokens[path] // a re-watch of the same path replaces
		oldCW := s.composedWatches[path]
		delete(s.composedWatches, path)
		s.watchTokens[path] = token
		s.watchMu.Unlock()
		if hadOld {
			s.server.coord.endRead(oldTok)
		}
		if oldCW != nil {
			oldCW.Stop()
		}

		if below := s.server.Mounts.MountsUnder(path); len(below) > 0 {
			s.startComposedWatch(req, below, token)
			return
		}

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

// startComposedWatch fans a client watch on an ancestor path across the base owner
// and every mount below it: it sends one client confirmation and one composed
// initial snapshot, then multiplexes the sub-watches' deltas into the single
// client watch. Because delta patches are root-rooted, a sub-watch event is
// forwarded with only its Path re-stamped to the client's watch path — the patch
// itself passes through unchanged. token is the coordinator reader already held
// for path.
//
// Event-preservation is a logd guarantee (single commit sequence); docd inherits it
// for single-route watches but is best-effort across mount boundaries. A composed
// watch is event-preserving while its mount membership is stable and during live
// streaming, but a membership change ends it (see terminateWatch) and the re-watch
// RE-INITS with a fresh composed snapshot rather than replaying the gap. FromCommit is
// therefore not honored here: the sub-streams have independent commit sequences, so a
// single resume commit cannot replay them, and re-init (a re-sync to current state)
// sidesteps that. A snapshot-diffing consumer reconciles the re-init with no gap.
func (s *ClientSession) startComposedWatch(req *logdapi.SessionRequest, below []*MountEntry, token uint64) {
	clientID := req.ID
	path := req.Watch.Path

	owner, pFields, errResp := s.composeCheck(clientID, path, below)
	if errResp != nil {
		s.releaseWatchToken(path)
		_ = s.writeToClient(errResp)
		return
	}

	cw := &composedWatch{path: path, client: s}

	// Establish sub-watches FIRST — their deltas buffer in cw — so no change
	// between the snapshot and going live is missed. NoInit on each: the composer
	// supplies the single initial snapshot below.
	watchReq := func(p string) *logdapi.SessionRequest {
		return &logdapi.SessionRequest{Scope: s.clientScope, Watch: &logdapi.WatchRequest{Path: p, NoInit: true}}
	}
	if owner == nil {
		ls, err := startLogdWatchStream(s.logdAddr, path, s.clientScope, cw.forward)
		if err != nil {
			s.releaseWatchToken(path)
			_ = s.writeToClient(logdapi.NewErrorResponse(clientID, logdapi.ErrCodeSessionClosed, err.Error()))
			return
		}
		cw.addStop(ls.Stop)
	} else {
		ms := owner.Session
		id := ms.RouteWatchStream(watchReq(path), cw.forward)
		cw.addStop(func() { ms.stopWatchStream(id, path) })
	}
	for _, m := range below {
		ms, mp := m.Session, m.Path
		id := ms.RouteWatchStream(watchReq(mp), cw.forward)
		cw.addStop(func() { ms.stopWatchStream(id, mp) })
	}

	// Register cw for teardown — unless the session is closing or this watch was
	// force-ended during setup (its token replaced/dropped), in which case tear the
	// freshly-built sub-watches back down.
	s.watchMu.Lock()
	if s.closing || s.watchTokens[path] != token {
		s.watchMu.Unlock()
		cw.Stop()
		return
	}
	s.composedWatches[path] = cw
	s.watchMu.Unlock()

	_ = s.writeToClient(logdapi.NewWatchResponse(clientID, path, nil))
	if !req.Watch.NoInit {
		// nil commit: a composed watch's initial state is always current (it re-inits
		// to current on membership change rather than replaying a historical commit).
		if root, commit, err := s.composeReadTree(path, owner, below, pFields, nil); err == nil {
			_ = s.writeToClient(logdapi.NewStateEvent(commit, path, root))
		}
	}
	cw.begin()
}

// composedWatch multiplexes several backend sub-watches into one client watch.
type composedWatch struct {
	path   string
	client *ClientSession

	mu       sync.Mutex
	started  bool
	buffered []*logdapi.SessionResponse
	stops    []func()
}

// forward is the sink for every sub-watch. A sub-watch failure ends the whole
// composed watch (the client re-establishes and re-composes). Otherwise it drops
// non-events (sub-watch confirmations), re-stamps a delta event's path to the
// composed watch's path, and buffers it until the initial snapshot has been sent,
// then forwards live.
func (cw *composedWatch) forward(resp *logdapi.SessionResponse) {
	if resp.Error != nil {
		reason := resp.Error.Code
		if reason == "" {
			reason = "watch_ended"
		}
		cw.client.terminateWatch(cw.path, reason)
		return
	}
	if resp.Event == nil {
		return
	}
	ev := *resp.Event
	ev.Path = cw.path // patch is root-rooted; only the routing path is re-stamped
	out := &logdapi.SessionResponse{Event: &ev}

	cw.mu.Lock()
	if !cw.started {
		cw.buffered = append(cw.buffered, out)
		cw.mu.Unlock()
		return
	}
	cw.mu.Unlock()
	_ = cw.client.writeToClient(out)
}

func (cw *composedWatch) addStop(fn func()) {
	cw.mu.Lock()
	cw.stops = append(cw.stops, fn)
	cw.mu.Unlock()
}

// begin flushes the deltas buffered during setup and switches to live forwarding,
// after the confirmation and initial snapshot have been written.
func (cw *composedWatch) begin() {
	cw.mu.Lock()
	cw.started = true
	buf := cw.buffered
	cw.buffered = nil
	cw.mu.Unlock()
	for _, out := range buf {
		_ = cw.client.writeToClient(out)
	}
}

// Stop tears down every sub-watch.
func (cw *composedWatch) Stop() {
	cw.mu.Lock()
	stops := cw.stops
	cw.stops = nil
	cw.mu.Unlock()
	for _, fn := range stops {
		fn()
	}
}

// releaseWatchToken releases the coordinator reader token the client holds for
// path and tears down a composed watch if one is registered there. Safe to call
// when none is held (an unwatch of a base path docd did not track, or a watch
// already force-ended).
func (s *ClientSession) releaseWatchToken(path string) {
	s.watchMu.Lock()
	token, ok := s.watchTokens[path]
	if ok {
		delete(s.watchTokens, path)
	}
	cw := s.composedWatches[path]
	delete(s.composedWatches, path)
	s.watchMu.Unlock()
	s.lastSeenMu.Lock()
	delete(s.lastSeen, path)
	s.lastSeenMu.Unlock()
	if ok {
		s.server.coord.endRead(token)
	}
	if cw != nil {
		cw.Stop()
	}
}

// forceWatch is the coordinator's force-teardown hook: a mount/unmount whose
// force_after elapsed is ending this watch so its membership can change.
func (s *ClientSession) forceWatch(path string) {
	s.terminateWatch(path, "membership_changed")
}

// terminalWatchEvent is the id-less event that tells a client its watch on path
// has ended (and why), so it re-establishes — the client routes it to the watch
// by path, as with any watch event. commit is the highest commit delivered on the
// watch, a resume point the client can re-watch FromCommit for a gapless reconnect.
func terminalWatchEvent(path, reason string, commit int64) *logdapi.SessionResponse {
	return &logdapi.SessionResponse{Event: &logdapi.WatchEvent{Path: path, Commit: commit, Ended: true, EndReason: reason}}
}

// terminateWatch ends a client watch — whether forced by a mount/unmount, failed
// by a crashed controller, or a base sub-watch dropping — by releasing its
// coordinator reader token, telling the client the watch ended so it
// re-establishes, and tearing down the backend (a composed watch's sub-watches,
// or a single-route watch's one backend). Idempotent: only the first call for a
// path does the work.
func (s *ClientSession) terminateWatch(path, reason string) {
	s.watchMu.Lock()
	token, ok := s.watchTokens[path]
	if ok {
		delete(s.watchTokens, path)
	}
	cw := s.composedWatches[path]
	delete(s.composedWatches, path)
	s.watchMu.Unlock()
	if !ok {
		return // already ended (raced with unwatch/close/another sub-failure)
	}
	s.lastSeenMu.Lock()
	commit := s.lastSeen[path]
	delete(s.lastSeen, path)
	s.lastSeenMu.Unlock()

	s.server.coord.endRead(token) // no-op if the coordinator already forced it
	_ = s.writeToClient(terminalWatchEvent(path, reason, commit))

	if cw != nil {
		cw.Stop()
		return
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
// reader token and composed watch it still holds, so a departing client neither
// wedges mounts nor leaks sub-watch connections.
func (s *ClientSession) releaseAllWatches() {
	s.watchMu.Lock()
	s.closing = true
	tokens := make([]uint64, 0, len(s.watchTokens))
	for _, t := range s.watchTokens {
		tokens = append(tokens, t)
	}
	s.watchTokens = make(map[string]uint64)
	cws := make([]*composedWatch, 0, len(s.composedWatches))
	for _, cw := range s.composedWatches {
		cws = append(cws, cw)
	}
	s.composedWatches = make(map[string]*composedWatch)
	s.watchMu.Unlock()
	s.lastSeenMu.Lock()
	s.lastSeen = make(map[string]int64)
	s.lastSeenMu.Unlock()
	for _, t := range tokens {
		s.server.coord.endRead(t)
	}
	for _, cw := range cws {
		cw.Stop()
	}
}
