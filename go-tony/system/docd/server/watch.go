package server

import (
	"fmt"
	"sync"

	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// clientWatch is docd's per-watch bookkeeping. A session may hold several, even on
// one path; each is keyed by its watch key and holds its own coordinator reader
// token. cw is nil for a single-route watch.
type clientWatch struct {
	token    uint64
	path     string
	clientID *string // the client's watch request id, stamped on events and terminals
	cw       *composedWatch
}

// watchKeyFor is the s.watches map key for a watch: the client's request id when
// present (so several watches on one path stay distinct), else the path (at most
// one id-less watch per path). The "id:"/"path:" prefixes keep the namespaces
// apart.
func watchKeyFor(id *string, path string) string {
	if id != nil {
		return "id:" + *id
	}
	return "path:" + path
}

// coordinateWatch registers a client watch as a reader with the mount coordinator
// (so a concurrent mount/unmount on an overlapping path drains or force-ends it),
// then serves it — composed across the base owner and mounts below when the watch
// path is a strict ancestor of one or more mounts, or single-routed otherwise.
//
// Admission runs on its own goroutine so writer priority — a pending overlapping
// mount holding off new watches — does not stall the client's request loop. The
// watch is registered under its key before it is served, so releaseWatchToken and
// close reliably find it. A session already closing when admission completes
// releases the token instead of leaking it.
func (s *ClientSession) coordinateWatch(req *logdapi.SessionRequest) {
	path := req.Watch.Path
	clientID := req.ID
	key := watchKeyFor(clientID, path)
	go func() {
		token, ok := s.server.coord.beginRead(path, func() { s.terminateWatch(key, "membership_changed") })
		if !ok {
			_ = s.writeToClient(logdapi.NewErrorResponse(clientID, logdapi.ErrCodeInvalidPath,
				fmt.Sprintf("invalid watch path %q", path)))
			return
		}

		s.watchMu.Lock()
		if s.closing {
			s.watchMu.Unlock()
			s.server.coord.endRead(token)
			return
		}
		old := s.watches[key] // a same-key re-watch replaces; distinct ids coexist
		s.watches[key] = &clientWatch{token: token, path: path, clientID: clientID}
		s.watchMu.Unlock()
		if old != nil {
			s.server.coord.endRead(old.token)
			if old.cw != nil {
				old.cw.Stop()
			}
		}

		if below := s.server.Mounts.MountsUnder(path); len(below) > 0 {
			s.startComposedWatch(req, below, token, key)
			return
		}

		switch dest, entry := s.routeFor(req); dest {
		case destController:
			entry.Session.RouteRequest(s, req)
		case destUnavailable:
			s.releaseWatchToken(key)
			_ = s.writeToClient(logdapi.NewErrorResponse(clientID, logdapi.ErrCodeUnavailable,
				fmt.Sprintf("controller for %q is unavailable", entry.Path)))
		case destMeta:
			s.releaseWatchToken(key)
			_ = s.writeToClient(logdapi.NewErrorResponse(clientID, logdapi.ErrCodeUnsupported,
				"cannot watch .meta"))
		default: // destLogd
			if err := s.writeToLogd(req); err != nil {
				s.releaseWatchToken(key)
				_ = s.writeToClient(logdapi.NewErrorResponse(clientID, logdapi.ErrCodeSessionClosed, err.Error()))
			}
		}
	}()
}

// startComposedWatch fans a client watch on an ancestor path across the base owner
// and every mount below it: it sends one client confirmation and one composed
// initial snapshot, then multiplexes the sub-watches' deltas into the single
// client watch. Because delta patches are root-rooted, a sub-watch event is
// forwarded with only its Path re-stamped to the client's watch path (and the
// client's watch id stamped for routing) — the patch itself passes through
// unchanged. token/key identify the coordinator reader already held for this watch.
//
// Event-preservation is a logd guarantee (single commit sequence); docd inherits it
// for single-route watches but is best-effort across mount boundaries. A composed
// watch is event-preserving while its mount membership is stable and during live
// streaming, but a membership change ends it (see terminateWatch) and the re-watch
// RE-INITS with a fresh composed snapshot rather than replaying the gap. FromCommit is
// therefore not honored here: the sub-streams have independent commit sequences, so a
// single resume commit cannot replay them, and re-init (a re-sync to current state)
// sidesteps that. A snapshot-diffing consumer reconciles the re-init with no gap.
func (s *ClientSession) startComposedWatch(req *logdapi.SessionRequest, below []*MountEntry, token uint64, key string) {
	clientID := req.ID
	path := req.Watch.Path

	owner, pFields, errResp := s.composeCheck(clientID, path, below)
	if errResp != nil {
		s.releaseWatchToken(key)
		_ = s.writeToClient(errResp)
		return
	}

	cw := &composedWatch{path: path, key: key, clientID: clientID, client: s}

	// Establish sub-watches FIRST — their deltas buffer in cw — so no change
	// between the snapshot and going live is missed. NoInit on each: the composer
	// supplies the single initial snapshot below.
	watchReq := func(p string) *logdapi.SessionRequest {
		return &logdapi.SessionRequest{Scope: s.clientScope, Watch: &logdapi.WatchRequest{Path: p, NoInit: true}}
	}
	if owner == nil {
		ls, err := startLogdWatchStream(s.logdAddr, path, s.clientScope, cw.forward)
		if err != nil {
			s.releaseWatchToken(key)
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
	// force-ended/replaced during setup (its entry gone or holding a new token), in
	// which case tear the freshly-built sub-watches back down.
	s.watchMu.Lock()
	w := s.watches[key]
	if s.closing || w == nil || w.token != token {
		s.watchMu.Unlock()
		cw.Stop()
		return
	}
	w.cw = cw
	s.watchMu.Unlock()

	_ = s.writeToClient(logdapi.NewWatchResponse(clientID, path, nil))
	if !req.Watch.NoInit {
		// nil commit: a composed watch's initial state is always current (it re-inits
		// to current on membership change rather than replaying a historical commit).
		if root, commit, err := s.composeReadTree(path, owner, below, pFields, nil); err == nil {
			_ = s.writeToClient(logdapi.NewStateEvent(clientID, commit, path, root))
		}
	}
	cw.begin()
}

// composedWatch multiplexes several backend sub-watches into one client watch.
type composedWatch struct {
	path     string
	key      string
	clientID *string
	client   *ClientSession

	mu       sync.Mutex
	started  bool
	buffered []*logdapi.SessionResponse
	stops    []func()
}

// forward is the sink for every sub-watch. A sub-watch failure ends the whole
// composed watch (the client re-establishes and re-composes). Otherwise it drops
// non-events (sub-watch confirmations), re-stamps a delta event's path to the
// composed watch's path and its id to the client's watch id, and buffers it until
// the initial snapshot has been sent, then forwards live.
func (cw *composedWatch) forward(resp *logdapi.SessionResponse) {
	if resp.Error != nil {
		reason := resp.Error.Code
		if reason == "" {
			reason = "watch_ended"
		}
		cw.client.terminateWatch(cw.key, reason)
		return
	}
	if resp.Event == nil {
		return
	}
	ev := *resp.Event
	ev.Path = cw.path // patch is root-rooted; only the routing path is re-stamped
	out := &logdapi.SessionResponse{ID: cw.clientID, Event: &ev}

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

// releaseWatchToken releases the coordinator reader token for the watch with the
// given key and tears down its composed watch if one is registered. Safe to call
// when none is held (an unwatch of a base path docd did not track, or a watch
// already force-ended).
func (s *ClientSession) releaseWatchToken(key string) {
	s.watchMu.Lock()
	w, ok := s.watches[key]
	if ok {
		delete(s.watches, key)
	}
	s.watchMu.Unlock()
	s.lastSeenMu.Lock()
	delete(s.lastSeen, key)
	s.lastSeenMu.Unlock()
	if ok {
		s.server.coord.endRead(w.token)
		if w.cw != nil {
			w.cw.Stop()
		}
	}
}

// terminalWatchEvent is the event that tells a client its watch has ended (and
// why), so it re-establishes. id, when non-nil, is the client's watch id so the
// client routes it to the exact watch (several may share a path); commit is the
// highest commit delivered on the watch, a resume point for a gapless reconnect.
func terminalWatchEvent(id *string, path, reason string, commit int64) *logdapi.SessionResponse {
	return &logdapi.SessionResponse{ID: id, Event: &logdapi.WatchEvent{Path: path, Commit: commit, Ended: true, EndReason: reason}}
}

// terminateWatch ends a client watch — whether forced by a mount/unmount, failed
// by a crashed controller, or a base sub-watch dropping — by releasing its
// coordinator reader token, telling the client the watch ended so it
// re-establishes, and tearing down the backend (a composed watch's sub-watches,
// or a single-route watch's one backend). Idempotent: only the first call for a
// key does the work.
func (s *ClientSession) terminateWatch(key, reason string) {
	s.watchMu.Lock()
	w, ok := s.watches[key]
	if ok {
		delete(s.watches, key)
	}
	s.watchMu.Unlock()
	if !ok {
		return // already ended (raced with unwatch/close/another sub-failure)
	}
	s.lastSeenMu.Lock()
	commit := s.lastSeen[key]
	delete(s.lastSeen, key)
	s.lastSeenMu.Unlock()

	s.server.coord.endRead(w.token) // no-op if the coordinator already forced it
	_ = s.writeToClient(terminalWatchEvent(w.clientID, w.path, reason, commit))

	if w.cw != nil {
		w.cw.Stop()
		return
	}
	unwatch := &logdapi.SessionRequest{Unwatch: &logdapi.UnwatchRequest{Path: w.path, WatchID: w.clientID}}
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
	ws := make([]*clientWatch, 0, len(s.watches))
	for _, w := range s.watches {
		ws = append(ws, w)
	}
	s.watches = make(map[string]*clientWatch)
	s.watchMu.Unlock()
	s.lastSeenMu.Lock()
	s.lastSeen = make(map[string]int64)
	s.lastSeenMu.Unlock()
	for _, w := range ws {
		s.server.coord.endRead(w.token)
		if w.cw != nil {
			w.cw.Stop()
		}
	}
}
