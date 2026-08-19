package server

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"

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
		// The reason comes from the writer that forces us — session_mounted or
		// session_unmounted — so the client learns WHICH way the mount set moved
		// under it, not merely that it moved.
		token, ok := s.server.coord.beginRead(path, func(reason string) { s.terminateWatch(key, reason) })
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
// Mounts share the commit sequence for their lifetime: docd allocates a tx id from logd,
// every participant commits through that one logd under it, and the transaction is
// all-or-nothing (coordinatePatch). A commit number therefore means the same thing to
// every mount, which is what makes a composed read at a commit well defined
// (coordinateMatch) and a composed replay from one equally so.
//
// What a composed watcher must account for is MEMBERSHIP: a mount arriving or leaving
// mid-watch. It is told -- the watch ends with session_mounted or session_unmounted and
// the re-watch composes the new membership. That is the whole of it.
//
// FromCommit is honoured: docd resolves it to ONE absolute commit (a relative -N against
// the watermark, clamped to the retained floor, both of which a ping answers from memory),
// reads the composed initial state AT that commit, and starts every sub-watch replaying
// from it. The replayed deltas are flushed in COMMIT ORDER before going live, which is
// meaningful precisely because the mounts share the sequence -- and the client is sent one
// replayComplete for the composed replay rather than one per sub-stream
// (4ses3fqsh12ks8awgnn0).
func (s *ClientSession) startComposedWatch(req *logdapi.SessionRequest, below []*MountEntry, token uint64, key string) {
	clientID := req.ID
	path := req.Watch.Path

	// Resolve the cursor to one absolute commit for every sub-watch to share. A
	// relative -N is resolved here rather than by each sub-watch, so they all replay
	// from the same place as the composed initial state (see logdWatermark).
	var from, replayingTo *int64
	if fc := req.Watch.FromCommit; fc != nil {
		head, floor, err := logdWatermark(s.logdAddr, matchReadTimeout)
		if err != nil {
			s.releaseWatchToken(key)
			_ = s.writeToClient(logdapi.NewErrorResponse(clientID, logdapi.ErrCodeSessionClosed,
				fmt.Sprintf("cannot resolve the watch cursor: %v", err)))
			return
		}
		start := *fc
		if start < 0 {
			// Relative: the last N commits, clamped to what is retained. Clamped
			// rather than refused, as logd does for the same request.
			start = head + start
			if start < floor {
				start = floor
			}
			if start < 0 {
				start = 0
			}
		}
		s.log.Debug("composed watch cursor", "path", path, "asked", *fc,
			"watermark", head, "floor", floor, "from", start)
		from = &start
		if head > start {
			h := head
			replayingTo = &h
		}
	}

	owner, pFields, errResp := s.composeCheck(clientID, path, below)
	if errResp != nil {
		s.releaseWatchToken(key)
		_ = s.writeToClient(errResp)
		return
	}

	cw := &composedWatch{path: path, key: key, clientID: clientID, client: s, replaying: from != nil}

	// The sub-watch on the composed path itself sees the WHOLE subtree, mounts
	// included: a logd-backed mount commits to the same logd, so its deltas come back
	// on this stream as well as on the mount's own. Forwarding both delivered every
	// commit twice -- harmless for a field write, wrong for an operation, since
	// !arraydiff applied twice is not !arraydiff applied once (hs9fge9rh12ksztzgnn0).
	//
	// So this stream is trimmed to what the composed path owns, using the same
	// partition that decomposes a WRITE across mounts -- one rule for both directions.
	// A delta which cannot be split (an op above a mount boundary) is forwarded whole:
	// duplicating it is better than dropping the part nobody else carries, and it says
	// so.
	var mounts []mountInfo
	for _, m := range below {
		mf, ferr := pathFields(m.Path)
		if ferr != nil {
			continue
		}
		mounts = append(mounts, mountInfo{entry: m, segs: mf})
	}
	blocks := s.server.patchTagFilter()
	cw.trimOwned = func(patch *ir.Node) *ir.Node {
		if patch == nil || len(mounts) == 0 {
			return patch
		}
		_, base, err := partition(patch, nil, mounts, blocks)
		if err != nil {
			s.log.Warn("composed watch cannot separate a delta from the mounts below it; forwarding it whole",
				"path", path, "error", err)
			return patch
		}
		return base
	}

	// Establish sub-watches FIRST — their deltas buffer in cw — so no change
	// between the snapshot and going live is missed. NoInit on each: the composer
	// supplies the single initial snapshot below.
	watchReq := func(p string) *logdapi.SessionRequest {
		return &logdapi.SessionRequest{Scope: s.clientScope,
			Watch: &logdapi.WatchRequest{Path: p, NoInit: true, FromCommit: from}}
	}
	if owner == nil {
		ls, err := startLogdWatchStream(s.logdAddr, path, s.clientScope, from, cw.forwardOwned)
		if err != nil {
			s.releaseWatchToken(key)
			_ = s.writeToClient(logdapi.NewErrorResponse(clientID, logdapi.ErrCodeSessionClosed, err.Error()))
			return
		}
		cw.addStop(ls.Stop)
	} else {
		ms := owner.Session
		id := ms.RouteWatchStream(watchReq(path), cw.forwardOwned)
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

	_ = s.writeToClient(logdapi.NewWatchResponseFrom(clientID, path, from, replayingTo))
	if !req.Watch.NoInit {
		// The state the deltas apply to: at the resolved commit when resuming, and at
		// current otherwise. A membership change still re-inits at current, because the
		// composition changed and deltas from before it describe a different document.
		root, commit, err := s.composeReadTree(path, owner, below, pFields, from)
		if err != nil {
			// The initial snapshot IS a match, and a watch whose match failed has no
			// baseline. Deltas against a baseline the client never received would be
			// applied to whatever it already held, so it is better to end the watch
			// than to stream into a state nobody established. The confirmation is
			// already out, so this ends the watch rather than answering the request.
			s.terminateWatch(key, logdapi.ErrCodeMatchFailed)
			return
		}
		_ = s.writeToClient(logdapi.NewStateEvent(clientID, commit, path, root))
	}
	cw.begin(len(below) + 1)
}

// composedReplayWait bounds how long a composed watch waits for its sub-streams to finish
// replaying before flushing what has arrived. A replay is bounded work, so this is a
// backstop against a participant which does not report rather than a timeout anyone should
// reach.
const composedReplayWait = 10 * time.Second

// composedWatch multiplexes several backend sub-watches into one client watch.
type composedWatch struct {
	path     string
	key      string
	clientID *string
	client   *ClientSession

	// replaying says the sub-watches were given a cursor, so each will finish its
	// replay with a replayComplete of its own and the client is owed exactly one for
	// the composed replay.
	replaying bool

	// trimOwned removes the subtrees owned by mounts below from a delta on the
	// composed path's own stream. See where it is built.
	trimOwned func(*ir.Node) *ir.Node

	mu        sync.Mutex
	started   bool
	buffered  []*logdapi.SessionResponse
	stops     []func()
	replayed  int // sub-streams which have reported replayComplete
	subs      int // sub-streams expected, once known (see begin)
	subsKnown bool
	flushed   bool
}

// forwardOwned is the sink for the sub-watch on the composed path itself: it keeps only
// what that path owns, since the mounts below carry their own subtrees on their own
// streams. A delta which is entirely a mount's is dropped here -- it is not lost, it
// arrives on that mount's stream.
func (cw *composedWatch) forwardOwned(resp *logdapi.SessionResponse) {
	if resp.Event != nil && resp.Event.Patch != nil && cw.trimOwned != nil {
		kept := cw.trimOwned(resp.Event.Patch)
		if kept == nil {
			return
		}
		ev := *resp.Event
		ev.Patch = kept
		trimmed := *resp
		trimmed.Event = &ev
		resp = &trimmed
	}
	cw.forward(resp)
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

	// A sub-stream's replayComplete is its own, not the composed watch's: the client
	// gets one when every sub-stream has finished, which is what flushIfReplayed
	// decides. Counted, not forwarded.
	if ev.ReplayComplete {
		cw.mu.Lock()
		cw.replayed++
		cw.mu.Unlock()
		cw.flushIfReplayed()
		return
	}

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
// after the confirmation and initial snapshot have been written. subs is how many
// sub-streams the watch is composed of.
//
// Without a cursor there is nothing to wait for: the buffer holds whatever landed during
// setup and it goes out at once. With one, each sub-stream is replaying its own history
// and the buffer is not complete until all of them have said so -- so the flush waits,
// and then goes out in COMMIT ORDER, which is meaningful because the mounts share the
// sequence. A sub-stream which never reports is waited for and no longer: whatever has
// arrived is flushed, in order, rather than a watch that never starts.
func (cw *composedWatch) begin(subs int) {
	cw.mu.Lock()
	cw.subs, cw.subsKnown = subs, true
	replaying, replayed := cw.replaying, cw.replayed
	cw.mu.Unlock()

	if !replaying {
		cw.flush(false)
		return
	}
	if replayed >= subs {
		cw.flush(true)
		return
	}
	go func() {
		deadline := time.After(composedReplayWait)
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				cw.mu.Lock()
				done := cw.replayed >= cw.subs || cw.flushed
				cw.mu.Unlock()
				if done {
					cw.flushIfReplayed()
					return
				}
			case <-deadline:
				cw.client.log.Warn("composed watch replay did not complete on every sub-stream; flushing what arrived",
					"path", cw.path, "reported", cw.replayed, "subStreams", cw.subs)
				cw.flush(true)
				return
			}
		}
	}()
}

// flushIfReplayed flushes once every sub-stream has finished replaying. Called from a
// sub-stream's replayComplete and from begin's wait, either of which may be last.
func (cw *composedWatch) flushIfReplayed() {
	cw.mu.Lock()
	ready := cw.subsKnown && cw.replaying && cw.replayed >= cw.subs && !cw.flushed
	cw.mu.Unlock()
	if ready {
		cw.flush(true)
	}
}

// flush sends the buffered deltas and switches to live forwarding. byCommit orders them
// first, and sends the client the one replayComplete it is owed.
func (cw *composedWatch) flush(byCommit bool) {
	cw.mu.Lock()
	if cw.flushed {
		cw.mu.Unlock()
		return
	}
	cw.flushed = true
	cw.started = true
	buf := cw.buffered
	cw.buffered = nil
	cw.mu.Unlock()

	if byCommit {
		// Stable, so two events at one commit -- a transaction across mounts is ONE
		// commit, so its participants' deltas arrive with the same number -- keep the
		// order they arrived in.
		sort.SliceStable(buf, func(i, j int) bool {
			return buf[i].Event.Commit < buf[j].Event.Commit
		})
	}
	for _, out := range buf {
		_ = cw.client.writeToClient(out)
	}
	if byCommit {
		_ = cw.client.writeToClient(logdapi.NewReplayCompleteEvent(cw.clientID, cw.path))
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
