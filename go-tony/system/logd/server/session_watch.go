package server

import (
	"errors"
	"fmt"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// Watching: establishing a watch, and the goroutine that serves it.
//
// A watch is registered on the request loop and served from its own goroutine, which
// sends the initial state, replays history when the client asked to resume, and then
// forwards live commits until the session ends or the client falls behind.

// handleWatch handles watch requests.
func (s *Session) handleWatch(id *string, req *api.WatchRequest) {
	path := req.Path

	// Check if session using pending is still valid
	if errMsg := s.checkPendingValid(); errMsg != "" {
		s.sendError(id, api.ErrCodeMigrationAborted, errMsg)
		return
	}

	// Validate path
	if path != "" {
		if err := validateDataPath(path); err != nil {
			s.sendError(id, api.ErrCodeInvalidPath, err.Error())
			return
		}
	}

	// Admission: a path is either a single id-less watch or N distinct id-bearing
	// watches, never mixed, so events route unambiguously. Reject an id-less watch
	// when the path is already watched, an id-bearing watch when an id-less watch
	// holds the path, and any request whose id duplicates an existing watch.
	s.watchMu.RLock()
	var reject string
	for _, w := range s.watches {
		if id != nil && w.ID != nil && *w.ID == *id {
			reject = fmt.Sprintf("already watching with id %q", *id)
			break
		}
		if w.Path != path {
			continue
		}
		if id == nil {
			reject = fmt.Sprintf("already watching %q", path)
			break
		}
		if w.ID == nil {
			reject = fmt.Sprintf("%q already has an id-less watch", path)
			break
		}
	}
	s.watchMu.RUnlock()
	if reject != "" {
		s.sendError(id, api.ErrCodeAlreadyWatching, reject)
		return
	}

	// IMPORTANT: Register with hub FIRST to avoid race condition.
	// Events that arrive between Watch and GetCurrentCommit will be queued.
	// After replay, we skip any queued events with commit <= currentCommit.
	// Buffer sized for burst tolerance: Broadcast is non-blocking and fails a watcher whose
	// buffer is full (see WatchHub.Broadcast), so the buffer — not a time grace — is what
	// absorbs a transient read stall before the watch is failed.
	watcher := NewWatcher(path, s.scopeID(), req.FromCommit, 1024)
	watcher.ID = id
	s.hub.Watch(watcher)

	// Now get current commit - this is our replay target
	currentCommit, err := s.storage.GetCurrentCommit()
	if err != nil {
		s.hub.Unwatch(watcher)
		s.sendError(id, api.ErrCodeStorage, fmt.Sprintf("failed to get current commit: %v", err))
		return
	}

	// Store watcher
	s.watchMu.Lock()
	s.watches[watchKey(id, path)] = watcher
	s.watchMu.Unlock()

	// A NEGATIVE fromCommit is relative: -N asks for the last N commits, resolved
	// here, against the watermark this watch is being established at. It is clamped
	// rather than refused -- a client asking for a window is asking for what there is,
	// and it cannot name the retained floor by number because it does not know it. An
	// absolute cursor keeps its refusal (forwardEvents), because a client naming a
	// commit is claiming to know where it was.
	fromCommit := req.FromCommit
	if fromCommit != nil && *fromCommit < 0 {
		start := currentCommit + *fromCommit
		if floor := s.storage.ReplayFloor(); start < floor {
			start = floor
		}
		if start < 0 {
			start = 0
		}
		s.log.Debug("relative watch cursor", "path", path, "offset", *fromCommit,
			"watermark", currentCommit, "from", start)
		fromCommit = &start
		watcher.FromCommit = fromCommit
	}

	// Determine replay range
	var replayingTo, replayingFrom *int64
	if fromCommit != nil && *fromCommit < currentCommit {
		replayingTo = &currentCommit
		replayingFrom = fromCommit
	}

	// Send watch confirmation
	s.send(api.NewWatchResponseFrom(id, path, replayingFrom, replayingTo))

	// Start event forwarder goroutine
	go s.forwardEvents(watcher, fromCommit, req.NoInit, currentCommit)
}

// forwardEvents forwards events from a watcher to the session's outgoing channel.
// It handles initial state and replay, then forwards live events with deduplication.
//
// Race prevention: We registered with the hub BEFORE getting currentCommit.
// This means events that arrive between Watch and GetCurrentCommit are queued.
// After replay completes, we skip any queued events with commit <= currentCommit
// since they were already replayed.
//
// Error handling: If replay fails, an error event is sent and the watch is terminated.
// The client should re-establish the watch, possibly from a different commit.
// watchStream is one watch while it runs: the state it carries, and the jobs it does with
// that state -- send the initial picture, replay what the client missed, then stream what
// happens next, with the scoped/baseline difference running through all three.
//
// It is a type because those jobs were one 350-line function which repeated the same
// bookkeeping in each of them, and the bugs were in the repetitions rather than in the
// work: the resume point was advanced in three of the four places it had to be, so a scoped
// watch dropped after an hour told its client to resume from the commit it started at
// (ntadpaech12krandgsn0). Here each thing is done once -- accountFor, stepBaseline,
// emitScoped, fail -- and each job is a method a reader can hold in mind whole.
type watchStream struct {
	s       *Session
	watcher *Watcher
	path    string
	scoped  bool

	// A watch whose path has no value yet is the ordinary way to start watching
	// something that does not exist. Say it once, quietly, and say so again when it
	// arrives: the pair is a story an operator can follow, where the same line repeated
	// per event is just an alarm they learn to ignore.
	absent *watchAbsence

	// replayedThrough is the commit the replay covered, and the bound the live loop
	// dedups against: a write can race into [hub-register, GetCurrentCommit] and be
	// queued for delivery after it was already replayed.
	replayedThrough int64

	// delivered is the highest commit this watch has ACCOUNTED FOR -- handed to the
	// client on a terminal event as its resume point. It advances even for a commit that
	// produced no event for this path, because the watch is correct through that commit
	// and resuming above it skips replaying history the client already has. Zero until
	// the watch has caught up to anything, which is what a client that never got started
	// should resume from.
	//
	// It advances in ONE place: accountFor. That is the whole reason this type exists.
	delivered int64

	// prev is the watched path's own subtree at the last delivered commit. A scoped
	// watcher emits deltas by recompute-and-diff against it (it must reproduce the scoped
	// read at each commit; a raw committed delta does not, because scope writes shadow
	// baseline and !key merges are identity-based). A BASELINE watcher uses it as a change
	// GATE: a coarse wake plus the superset read can wake a watcher for a commit that only
	// touched a sibling under a shared ancestor, so before forwarding the raw committed
	// delta (which baseline keeps for op fidelity) it confirms this subtree changed. See
	// issue eagjggjdh12ksg00bsn0.
	//
	// cur is the whole document at that same commit, kept by a BASELINE watcher and
	// STEPPED: cur = Patch(cur, committedPatch), then trimmed for prev. That is the read
	// path's own fold, just not restarted from a snapshot every time. It replaces a full
	// ReadStateAt per event per watcher, which was O(patches since the last snapshot):
	// 1.6ms at 50 commits, 62ms at 1550.
	//
	// A SCOPED watcher cannot step that way: its view is baseline with the scope's writes
	// applied LAST, so they shadow baseline stickily, and folding a baseline patch into a
	// materialized scoped document would let a baseline write overwrite a leaf the scope
	// owns. It steps through a ScopedWatchStepper when the scope can be served that way,
	// and recomputes when it cannot.
	prev, cur *ir.Node
	seeded    bool
	stepper   *storage.ScopedWatchStepper
}

// forwardEvents serves one watch until the session ends, the client falls behind, or the
// watch is failed. Three jobs, in order, and the state between them is the stream's.
func (s *Session) forwardEvents(watcher *Watcher, fromCommit *int64, noInit bool, currentCommit int64) {
	w := &watchStream{
		s:       s,
		watcher: watcher,
		path:    watcher.Path,
		scoped:  s.scopeID() != nil,
		absent:  &watchAbsence{log: s.log, path: watcher.Path},
	}

	// Where the initial picture is taken, and what the replay covers.
	startCommit := currentCommit
	if fromCommit != nil {
		startCommit = *fromCommit
		w.replayedThrough = currentCommit
	}

	// Refuse a cursor below the retained delta window before sending ANYTHING. The replay
	// itself would catch this (ReadPatchesInRange returns ErrReplayCompacted), but only
	// after the initial state has gone out -- and a state read below the floor is itself
	// approximate, since compaction leaves historical reads at snapshot granularity.
	// Handing the client a state it cannot trust and then an error is worse than the error
	// alone.
	//
	// The bound matches the replay's: it reads [startCommit+1, ...], so a cursor AT the
	// floor is fine -- "I have through commit F" needs only the deltas above F, which are
	// intact -- and one below it is not.
	if fromCommit != nil {
		if floor := s.storage.ReplayFloor(); *fromCommit < floor {
			s.log.Warn("watch cursor below retained history", "path", w.path, "fromCommit", *fromCommit, "floor", floor)
			s.failWatch(watcher, api.ErrCodeReplayCompacted, fmt.Sprintf(
				"cannot replay from commit %d: delta history is retained only from commit %d; re-watch without fromCommit to re-initialize",
				*fromCommit, floor+1), 0)
			return
		}
	}

	if !noInit && !w.sendInitialState(startCommit) {
		return
	}
	if fromCommit != nil && !w.replay(startCommit, currentCommit) {
		return
	}
	w.live()
}

// accountFor records that the watch is correct through this commit. Every path through the
// stream ends here, including the ones that deliver nothing: a commit which changed nothing
// under this path, or which the pre-filter proved cannot reach it, still leaves the watch
// correct through it and is a valid resume point.
func (w *watchStream) accountFor(commit int64) {
	if commit > w.delivered {
		w.delivered = commit
	}
}

// fail ends the watch, handing the client the resume point it has earned.
func (w *watchStream) fail(code, format string, args ...any) {
	w.s.failWatch(w.watcher, code, fmt.Sprintf(format, args...), w.delivered)
}

// sendInitialState sends the state at the path as of commit, which is what every delta
// after it applies to. It answers false when the watch has been failed.
func (w *watchStream) sendInitialState(commit int64) bool {
	state := ir.Null() // an empty store has no state to read
	if commit != 0 {
		var err error
		state, err = w.s.readDocAt(w.path, commit)
		if err != nil {
			w.s.log.Error("failed to read state for init", "path", w.path, "commit", commit, "error", err)
			w.fail(api.ErrCodeReplayFailed, "failed to read state at commit %d: %v", commit, err)
			return false
		}
		if w.path != "" {
			// Extract the value at the path.
			//
			// A failure here says nothing about storage: the read above already
			// succeeded, and this is navigation of the document it returned. What it
			// says is which of three things is true, and they want three different
			// volumes -- see PathErrorKind.
			state, err = extractPathValue(state, w.path)
			if err != nil {
				var pe *PathError
				switch {
				case errors.As(err, &pe) && pe.Kind == PathBadSegment:
					// This one never resolves, so serving null forever would tell the
					// client its path is empty when it is invalid.
					w.s.log.Warn("watch path cannot be extracted", "path", w.path, "error", err)
					w.fail(api.ErrCodeInvalidPath, "%s", err.Error())
					return false
				case errors.As(err, &pe) && pe.Kind == PathTypeConflict:
					w.s.log.Warn("watched path is shadowed by a non-object", "path", w.path, "error", err)
				default:
					w.s.log.Debug("watched path has no value yet", "path", w.path, "detail", err.Error())
				}
				state = ir.Null()
				w.absent.arm()
			}
		}
	}
	w.s.send(api.NewStateEvent(w.watcher.ID, commit, w.path, state))
	w.accountFor(commit)
	return true
}

// seedAt establishes the documents deltas are taken against, at commit. It answers false
// when the watch has been failed.
//
// forLive says the stream goes straight from here to live events, which is what decides
// whether a scoped stepper is seeded: a stepper FOLDS each committed delta, so it is only
// correct if every commit after its position is folded into it. A replay does not fold --
// it recomputes the scoped view per commit -- so a stepper seeded before a replay would sit
// at the commit the replay started from while the view moved on without it, and the first
// live event would fold onto a document that is commits behind. A replay therefore seeds no
// stepper and the live events after it recompute, which is what they did before.
//
// A replay seeds at the commit it starts from. A live watch seeds LAZILY, at
// (firstEvent.commit - 1) rather than at the commit it started at, because a write can race
// into [hub-register, GetCurrentCommit] and be queued with a commit at or below it; seeding
// at the start would fold that write into the baseline and drop its delta (the scoped-watch
// drop-one-event regression). Lazy seeding makes every queued or live event a correct
// forward diff.
func (w *watchStream) seedAt(commit int64, forLive bool) bool {
	var err error
	if w.scoped {
		w.prev, err = w.s.scopedDocAt(w.path, commit)
		if err != nil {
			w.s.log.Error("failed to read scoped watch base", "path", w.path, "commit", commit, "error", err)
			w.fail(api.ErrCodeReplayFailed, "failed to read scoped state at commit %d: %v", commit, err)
			return false
		}
		// A stepper folds each event into a document the watcher keeps, instead of
		// recomputing the whole scoped view per event. It is unavailable for a scope the
		// overlay cannot serve, and then nothing changes: the recompute stays.
		if forLive {
			w.stepper, err = w.s.storage.NewScopedWatchStepper(*w.s.scopeID(), commit)
			if err != nil {
				w.s.log.Warn("scoped watch stepper unavailable; recomputing per event", "path", w.path, "error", err)
				w.stepper = nil
			}
		}
		w.seeded = true
		return true
	}
	w.cur, err = w.s.fullDocAt(commit)
	if err != nil {
		w.s.log.Error("failed to read watch base", "path", w.path, "commit", commit, "error", err)
		w.fail(api.ErrCodeReplayFailed, "failed to read state at commit %d: %v", commit, err)
		return false
	}
	w.prev = subtreeOf(w.cur, w.path)
	w.seeded = true
	return true
}

// stepBaseline advances a baseline watch by one commit's delta and sends it if this
// watcher's own subtree changed. patch is the raw committed delta, which baseline forwards
// verbatim to preserve op fidelity (!key and friends). It answers false when the watch has
// been failed.
//
// share says the patch is the hub's shared copy, which several watchers hold at once:
// encoding mutates a node's parent linkage (ir.FromMap), so two session writers serializing
// the same node race, and this watcher is handed its own copy. A replayed patch is read
// from the log for this watcher alone and needs none.
func (w *watchStream) stepBaseline(commit int64, patch *ir.Node, shared bool) bool {
	// Step the document by this commit's delta instead of rebuilding it from the last
	// snapshot. A committed patch is already stripped and private to the tick, so it
	// applies as-is and is not mutated by Patch.
	stepped, err := api.NextState(w.cur, patch)
	if err != nil {
		w.s.log.Error("failed to apply patch for watch", "path", w.path, "commit", commit, "error", err)
		w.fail(api.ErrCodeReplayFailed, "failed to apply patch at commit %d: %v", commit, err)
		return false
	}
	w.cur = stepped
	next := subtreeOf(w.cur, w.path)
	w.accountFor(commit)
	// api.SameState decides what counts as a change; see it for comments.
	if api.SameState(next, w.prev) {
		return true
	}
	w.prev = next
	w.absent.observe(w.prev)
	if shared {
		patch = patch.DeepCopy()
	}
	w.s.send(api.NewPatchEvent(w.watcher.ID, commit, w.path, patch))
	return true
}

// emitScoped advances a scoped watch by one commit and sends what changed under the path.
// notification is nil for a replayed commit, where there is no committed delta to fold and
// the view is recomputed. It answers false when the watch has been failed.
//
// The delta is recompute-and-diff against the previously emitted state either way: a
// scope's raw committed patch is not its delta, because scope writes shadow baseline
// stickily and !key merges are identity-based. A stepper removes the READ, not the diff.
func (w *watchStream) emitScoped(commit int64, notification *storage.CommitNotification) bool {
	var next *ir.Node
	var err error
	if notification == nil {
		next, err = w.s.emitScopedDelta(w.watcher.ID, w.path, commit, w.prev)
	} else {
		next, err = w.s.emitScopedDeltaStepped(w.watcher.ID, w.path, commit, w.prev, w.stepper, notification)
	}
	if err != nil {
		w.s.log.Error("failed to read scoped state for watch", "path", w.path, "commit", commit, "error", err)
		w.fail(api.ErrCodeReplayFailed, "failed to read scoped state at commit %d: %v", commit, err)
		return false
	}
	w.prev = next
	w.absent.observe(w.prev)
	w.accountFor(commit)
	return true
}

// replay sends the deltas the client missed, from the commit it resumed at up to the one
// the watch was established at, then says the replay is complete. It answers false when the
// watch has been failed.
func (w *watchStream) replay(from, to int64) bool {
	if !w.seedAt(from, false) {
		return false
	}
	if from < to {
		patches, err := w.s.storage.ReadPatchesInRange(w.path, from+1, to, w.s.scopeID())
		switch {
		case errors.Is(err, storage.ErrReplayCompacted):
			// The cursor predates the retained delta window, so the exact history it
			// asked for no longer exists. Say that specifically: a client told
			// replay_compacted re-watches without fromCommit and re-initializes, where
			// replay_failed reads as a transient fault worth retrying with the same
			// doomed cursor.
			w.s.log.Warn("watch replay below retained history", "path", w.path, "fromCommit", from, "error", err)
			w.fail(api.ErrCodeReplayCompacted,
				"cannot replay from commit %d: %v; re-watch without fromCommit to re-initialize", from, err)
			return false
		case err != nil:
			w.s.log.Error("failed to read patches for replay", "path", w.path, "from", from+1, "to", to, "error", err)
			w.fail(api.ErrCodeReplayFailed, "failed to read patches from commit %d to %d: %v", from+1, to, err)
			return false
		}
		for _, patch := range patches {
			ok := false
			if w.scoped {
				ok = w.emitScoped(patch.Commit, nil)
			} else {
				ok = w.stepBaseline(patch.Commit, patch.Patch, false)
			}
			if !ok {
				return false
			}
		}
	}
	w.s.send(api.NewReplayCompleteEvent(w.watcher.ID, w.path))
	return true
}

// live forwards commits as they happen, until the session ends, the client falls behind, or
// something fails.
func (w *watchStream) live() {
	for {
		select {
		case <-w.s.done:
			return
		case <-w.watcher.Failed:
			// Broadcast dropped this watcher because its buffer was full. Report it as
			// what it is: the client fell behind, and it can resume from what this watch
			// accounted for rather than re-reading the whole document.
			w.fail(api.ErrCodeSlowConsumer, "watch on %q dropped: consumer did not keep up", w.path)
			return
		case notification, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			// Already replayed: a write can race into [hub-register, GetCurrentCommit]
			// and be queued after the replay covered it.
			if notification.Commit <= w.replayedThrough {
				continue
			}
			// Cheap pre-filter: the coarse wake fires this watcher for every write under
			// a shared top-level subtree, and recomputing full state per event collapses
			// under many watches. If the committed delta clearly cannot reach this
			// watcher's subtree (a plain merge that misses the path), skip the recompute.
			// Conservative: any op tag or a non-navigable ancestor falls through to the
			// authoritative recompute. The watch is still correct through the commit.
			if !patchMayAffect(notification.Patch, w.path) {
				w.accountFor(notification.Commit)
				continue
			}
			if !w.seeded && !w.seedAt(notification.Commit-1, true) {
				return
			}
			ok = false
			if w.scoped {
				ok = w.emitScoped(notification.Commit, notification)
			} else {
				ok = w.stepBaseline(notification.Commit, notification.Patch, true)
			}
			if !ok {
				return
			}
		}
	}
}
func (s *Session) emitScopedDeltaStepped(id *string, path string, commit int64, prev *ir.Node,
	stepper *storage.ScopedWatchStepper, n *storage.CommitNotification) (*ir.Node, error) {
	if stepper == nil {
		return s.emitScopedDelta(id, path, commit, prev)
	}
	full, err := stepper.Step(n)
	if err != nil {
		return prev, err
	}
	return s.emitScopedDeltaFrom(id, path, commit, prev, subtreeOf(full, path))
}

func (s *Session) emitScopedDelta(id *string, path string, commit int64, prev *ir.Node) (*ir.Node, error) {
	newDoc, err := s.scopedDocAt(path, commit)
	if err != nil {
		return prev, err
	}
	return s.emitScopedDeltaFrom(id, path, commit, prev, newDoc)
}

// emitScopedDeltaFrom sends the change between prev and newDoc, both already trimmed to
// the watched path.
func (s *Session) emitScopedDeltaFrom(id *string, path string, commit int64, prev, newDoc *ir.Node) (*ir.Node, error) {
	// What counts as a change is api.SameState's to say, here and at the two watch
	// paths above and the head's agreement check in storage/head.go. See it for why
	// the answer counts comments.
	if api.SameState(newDoc, prev) {
		return prev, nil
	}
	// The delta carries what the equality counts, or the two disagree in the other
	// direction: a change SameState reports would be diffed away to nothing and the
	// watcher told a commit happened by a patch that changes nothing. Inert on a
	// document with no comments, like the equality above it.
	rooted, err := tx.RootPatchAt(path, tony.DiffWith(prev, newDoc, tony.DiffComments(true)))
	if err != nil {
		return prev, err
	}
	s.send(api.NewPatchEvent(id, commit, path, rooted))
	return newDoc, nil
}

// handleUnwatch handles unwatch requests.
func (s *Session) handleUnwatch(id *string, req *api.UnwatchRequest) {
	path := req.Path

	// req.WatchID targets one specific watch; without it, cancel every watch on the
	// path (the legacy id-less behavior, and a bulk unwatch).
	s.watchMu.Lock()
	var removed []*Watcher
	if req.WatchID != nil {
		key := watchKey(req.WatchID, path)
		if w, ok := s.watches[key]; ok {
			removed = append(removed, w)
			delete(s.watches, key)
		}
	} else {
		for k, w := range s.watches {
			if w.Path == path {
				removed = append(removed, w)
				delete(s.watches, k)
			}
		}
	}
	s.watchMu.Unlock()

	if len(removed) == 0 {
		s.sendError(id, api.ErrCodeNotWatching, fmt.Sprintf("not watching %q", path))
		return
	}

	for _, w := range removed {
		s.hub.Unwatch(w)
	}

	s.send(api.NewUnwatchResponse(id, path))
}

// cleanupWatches removes all watches on session close.
func (s *Session) cleanupWatches() {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()

	for key, watcher := range s.watches {
		s.hub.Unwatch(watcher)
		delete(s.watches, key)
	}
}

// failWatch terminates an established watch and tells the client, so it can re-establish.
//
// It sends a TERMINAL WATCH EVENT (Ended, with reason), not an error response. The
// distinction is the difference between the client finding out and not, and the failure
// it exists for is silent and not hypothetical: measured, a slice taking sustained writes
// lost 550 of 1000 events and never recovered. The path was —
//
//  1. logd fails a watcher whose buffer it cannot drain: Broadcast runs on the tick's
//     dispatcher and will not block on a slow consumer, so a full buffer drops the
//     watcher (see WatchHub.Broadcast, "fail it loudly").
//  2. "Loudly" meant an error response stamped with the watch's id.
//  3. libctl's read pump sends anything with no Event to deliverResponse, which looks the
//     id up in the table of in-flight REQUESTS. A watch id was never in that table — its
//     request completed when the watch opened — so the failure was logged as "dropping
//     response with no matching request" and thrown away.
//
// The client was then waiting on a watch the server had already abandoned, with no error
// and no events, forever. routeEvent handles Ended correctly and always did (it fails the
// Watch with a WatchEndedError and unregisters it); logd was the only sender not using it,
// while docd had been sending terminal events for mount-membership changes all along.
//
// An error response remains right for rejecting a watch REQUEST that is still in flight —
// handleWatch's admission checks — because that id is in the pending table.
//
// commit is the highest commit this watch accounted for, so the client can resume from it
// rather than re-reading everything; 0 when it never got that far. message is for the
// server log, since the terminal event carries a short reason code and no prose.
func (s *Session) failWatch(watcher *Watcher, reason, message string, commit int64) {
	s.log.Warn("watch ended", "path", watcher.Path, "reason", reason, "detail", message, "commit", commit)
	// Stamp the watch id so the client fails the right watch (several may share a path).
	s.send(api.NewEndedEvent(watcher.ID, watcher.Path, reason, commit))
	s.hub.Unwatch(watcher)
	s.watchMu.Lock()
	delete(s.watches, watchKey(watcher.ID, watcher.Path))
	s.watchMu.Unlock()
}
