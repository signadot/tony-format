package server

import (
	"errors"
	"fmt"
	"io"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/ident"
)

// Watching: establishing a watch, and the goroutine that serves it.
//
// A watch is registered on the request loop and served from its own goroutine, which
// sends the initial state, replays history when the client asked to resume, and then
// forwards live commits until the session ends or the client falls behind.

// handleWatch handles watch requests.
func (s *Session) handleWatch(id *string, req *api.WatchRequest) {
	path := req.Path

	// Validate path
	if path != "" {
		if err := validateDataPath(path); err != nil {
			s.sendError(id, api.ErrCodeInvalidPath, err.Error())
			return
		}
		// And spelled as the store spells it: an element of a keyed array is addressed by its
		// name, whichever sugar the client used (ident.CanonicalPath).
		if canon, err := ident.CanonicalPath(s.storage.SchemaFor(s.scopeID()), path); err != nil {
			s.sendError(id, api.ErrCodeInvalidPath, err.Error())
			return
		} else {
			path = canon
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
	// Events that arrive between Watch and GetCurrentCommit will be queued. With
	// fromCommit, the live loop skips a queued event the replay already covered
	// (commit <= currentCommit). Without it there is no replay to dedup against: the
	// live loop seeds at the first event's commit - 1 instead (watchStream.seedAt).
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

	// A watch that will be refused is refused HERE, before the confirmation, so that
	// Watch() means "this watch exists" rather than "the request was well formed".
	//
	// Refusing after the confirmation is refusing too late for a caller who has to decide
	// something on the answer: an HTTP endpoint bridging a watch to a stream has already
	// committed its status by then, and cannot go back and say 404. Waiting a moment for
	// a possible failure does not work either -- with noInit a path that exists and is
	// quiet has no first event, so there is no signal to wait for at any duration.
	//
	// It belongs here on its own terms, too: whether the path holds anything is a fact
	// about the start commit, which is already in hand, and not about the stream.
	//
	// Only asked when the answer could refuse. A client that said waitIfAbsent has told
	// us absence is not a refusal, and pays nothing for the question. One that did not
	// pays a narrow read here, which for a noInit watch is a read it would not otherwise
	// have done.
	// Asked of the HEAD, not of the cursor a replay starts from. "Is anything here?" is a
	// question about the path, and a client resuming from an old cursor is asking it about
	// the path it is resuming -- refusing because the path had not been written yet at
	// commit 0 would 404 every full replay of a path that exists. The seed still reports
	// the cursor's own commit, where absence is history rather than a refusal.
	if !req.WaitIfAbsent {
		if perr := s.absentAt(path, currentCommit); perr != nil {
			s.hub.Unwatch(watcher)
			s.watchMu.Lock()
			delete(s.watches, watchKey(id, path))
			s.watchMu.Unlock()
			s.sendError(id, api.ErrCodeNotFound, perr.Error())
			return
		}
	}

	// Send watch confirmation
	s.send(api.NewWatchResponseFrom(id, path, replayingFrom, replayingTo))

	// Start event forwarder goroutine
	go s.forwardEvents(watcher, fromCommit, req.NoInit, currentCommit)
}

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

	// prev is the watched path's own value at the last delivered commit, nil where the
	// path holds nothing, and it is all a watch holds: the client's own copy, which is
	// what every delta is a delta of (one_delta_shape.md, one rooting).
	//
	// A BASELINE watcher steps it by the stored delta PROJECTED at the path
	// (api.ProjectDelta): what the entry says at or below the path, re-rooted there,
	// which is also what the client is sent. A projection that says nothing is a commit
	// that did not reach the path, and costs nothing; one that is blocked -- an operator
	// above the path states the whole value there -- is answered by a read at the path,
	// which narrows, and the delta sent is the diff. Either way prev is the change GATE: a
	// write that restates a value is not a change, and is not delivered
	// (eagjggjdh12ksg00bsn0). No whole document is held for a watch, at any width.
	//
	// A SCOPED watcher steps too, in the three cases the footprint can vouch for
	// (step): its own scope's commit, whose delta applies last in the fold anyway; a
	// baseline commit that meets no statement of the scope; and a baseline commit under
	// the scope's claim, which is dropped. What is left -- a baseline delta that meets a
	// statement of the scope, where folding it in would let baseline overwrite what the
	// scope holds (9b2vpggxh12ks0qde5n0) -- re-reads the view at the path and sends the
	// difference, and that read folds the scope's live statements, not its history.
	prev   *ir.Node
	seeded bool
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
	// itself would catch this (storage.Deltas returns ErrReplayCompacted), but only
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
	var state *ir.Node // an empty store holds nothing, at any path
	if commit != 0 {
		var err error
		state, err = w.s.readValueAt(w.path, commit)
		if err != nil {
			// A PathError says which of three things is true about the path, and they
			// want three different volumes -- see PathErrorKind. Anything else is the
			// read failing, which fails the watch.
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
			case errors.As(err, &pe):
				w.s.log.Debug("watched path has no value yet", "path", w.path, "detail", err.Error())
			default:
				w.s.log.Error("failed to read state for init", "path", w.path, "commit", commit, "error", err)
				w.fail(api.ErrCodeReplayFailed, "failed to read state at commit %d: %v", commit, err)
				return false
			}
			// The wire says absent (WatchEvent.Absent), and the log keeps the story
			// beside it (watchAbsence).
			state = nil
			w.absent.arm()
		}
	}
	w.s.send(api.NewStateEvent(w.watcher.ID, commit, w.path, state))
	w.accountFor(commit)
	return true
}

// seedAt establishes the value deltas are taken against, at commit: one read at the
// watched path, in the session's view, nil where it holds nothing. It answers false when
// the watch has been failed.
//
// A replay seeds at the commit it starts from. A live watch seeds LAZILY, at
// (firstEvent.commit - 1) rather than at the commit it started at, because a write can race
// into [hub-register, GetCurrentCommit] and be queued with a commit at or below it; seeding
// at the start would fold that write into the base and drop its delta. Lazy seeding makes
// every queued or live event a correct forward step.
func (w *watchStream) seedAt(commit int64) bool {
	prev, err := w.s.scopedDocAt(w.path, commit)
	if err != nil {
		w.s.log.Error("failed to read watch base", "path", w.path, "commit", commit, "error", err)
		w.fail(api.ErrCodeReplayFailed, "failed to read state at commit %d: %v", commit, err)
		return false
	}
	w.prev = prev
	w.seeded = true
	return true
}

// stepBaseline advances a baseline watch by one commit's stored delta and sends what it
// says at the watched path, if that changed anything there. It answers false when the
// watch has been failed.
//
// shared says the delta is the hub's copy, which several watchers hold at once: encoding
// mutates a node's parent linkage (ir.FromMap), so what is sent is this watcher's own copy.
// A replayed delta is read from the log for this watcher alone and needs none.
func (w *watchStream) stepBaseline(commit int64, patch *ir.Node, shared bool) bool {
	at, _, ok := api.ProjectDelta(patch, w.path)
	if ok && at == nil {
		// The entry does not reach the path. Correct through this commit, nothing to say.
		w.accountFor(commit)
		return true
	}
	var next, delta *ir.Node
	if ok {
		delta = at
		if shared {
			delta = at.DeepCopy()
		}
		var err error
		next, err = applyAt(w.prev, delta)
		if err != nil {
			w.s.log.Error("failed to apply delta for watch", "path", w.path, "commit", commit, "error", err)
			w.fail(api.ErrCodeReplayFailed, "failed to apply delta at commit %d: %v", commit, err)
			return false
		}
	} else {
		// An operator above the path states the whole value there, and what that leaves
		// at the path is what a read at the path says.
		var err error
		next, err = w.s.scopedDocAt(w.path, commit)
		if err != nil {
			w.s.log.Error("failed to read state for watch", "path", w.path, "commit", commit, "error", err)
			w.fail(api.ErrCodeReplayFailed, "failed to read state at commit %d: %v", commit, err)
			return false
		}
		delta = deltaAt(w.prev, next)
	}
	w.accountFor(commit)
	// api.SameState decides what counts as a change; see it for comments. The value held
	// is next either way: stepping took the last one over (api.StepState), so it may be
	// compared and nothing more.
	same := api.SameState(next, w.prev)
	w.prev = next
	if same {
		return true
	}
	w.absent.observe(w.prev)
	w.s.send(patchEvent(w.watcher.ID, commit, w.path, delta, next == nil))
	return true
}

// applyAt folds a delta rooted at the path into the value there: a client's own step.
// A base that holds nothing is a null to fold onto, and a delta that deletes the path
// leaves nothing, which is not the null it would fold to (api/state.go). It takes prev
// over (api.StepState): the caller holds the answer instead.
func applyAt(prev, delta *ir.Node) (*ir.Node, error) {
	if n := ir.Uncomment(delta); n != nil && ir.TagHas(n.Tag, libdiff.DeleteTag) {
		return nil, nil
	}
	base := prev
	if base == nil {
		base = ir.Null()
	}
	return api.StepState(base, delta)
}

// deltaAt is the delta from prev to next, both the value at the watched path, rooted
// there. Absence is a side of the diff and is stated as what it is: a value arriving where
// there was none is an insert, a value leaving is a delete, and neither is a null, which is
// a value (api/state.go).
func deltaAt(prev, next *ir.Node) *ir.Node {
	if prev == nil || next == nil {
		return libdiff.MakeDiff(prev, next)
	}
	return tony.DiffWith(prev, next, tony.DiffComments(true))
}

// patchEvent is a delta event at path, saying absent when the path holds nothing after it.
func patchEvent(id *string, commit int64, path string, delta *ir.Node, absent bool) *api.SessionResponse {
	ev := api.NewPatchEvent(id, commit, path, delta)
	ev.Event.Absent = absent
	return ev
}

// step advances the watch by one commit and sends what changed under the path. It answers
// false when the watch has been failed.
//
// A baseline watch steps by the stored delta. A scoped watch steps by its own scope's
// delta the same way, since that delta applies last in the fold whatever baseline does;
// and by a baseline delta when the footprint says the scope has stated nothing at, above
// or beneath what the delta states under the path (storage.BaselineDeltaInScope). A
// baseline delta under the scope's claim is dropped: the view there is the claim. What
// remains overlaps a statement of the scope, and is answered by a read at the path.
func (w *watchStream) step(commit int64, patch *ir.Node, scopeID *string, shared bool) bool {
	if !w.scoped {
		return w.stepBaseline(commit, patch, shared)
	}
	mine := w.s.scopeID()
	if scopeID != nil && mine != nil && *scopeID == *mine {
		w.s.hub.stats.scopeStep.Add(1)
		return w.stepBaseline(commit, patch, shared)
	}
	if at, _, ok := api.ProjectDelta(patch, w.path); ok {
		if at == nil {
			w.accountFor(commit)
			return true
		}
		switch w.s.storage.BaselineDeltaInScope(*mine, w.path, at) {
		case storage.BaselineHidden:
			w.s.hub.stats.scopeDrop.Add(1)
			w.accountFor(commit)
			return true
		case storage.BaselineSteps:
			w.s.hub.stats.scopeStep.Add(1)
			return w.stepBaseline(commit, patch, shared)
		}
	}
	w.s.hub.stats.scopeReread.Add(1)
	return w.emitScoped(commit)
}

// emitScoped advances a scoped watch by one commit and sends what changed under the path.
// It answers false when the watch has been failed.
//
// The delta is recompute-and-diff against the previously emitted state: a scope's raw
// committed patch is not its delta, because scope writes shadow baseline stickily and
// !key merges are identity-based. So there is nothing to fold, and a live commit is
// served exactly as a replayed one is -- the committed delta is not an input here.
//
// The recompute is a read at the path (scopedDocAt -> readValueAt), which narrows; see
// watchStream for what that costs.
func (w *watchStream) emitScoped(commit int64) bool {
	next, err := w.s.emitScopedDelta(w.watcher.ID, w.path, commit, w.prev)
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

// errWatchEnded stops a streaming walk because the watch is already over: the step that
// returned false has failed it and told the client why, and there is nothing left for the
// caller to report.
var errWatchEnded = errors.New("watch ended")

// replay sends the deltas the client missed, from the commit it resumed at up to the one
// the watch was established at, then says the replay is complete. It answers false when the
// watch has been failed.
func (w *watchStream) replay(from, to int64) bool {
	if !w.seedAt(from) {
		return false
	}
	if from < to {
		// Streamed, not collected: the range is emitted as it is read, so a wide
		// catch-up costs one entry rather than the whole history
		// (89my9f0kh12ksqknjhn0). The watcher's bounded buffer is where a consumer
		// that cannot keep up is failed, which is the existing contract -- the
		// server does not hold the range on its behalf.
		err := w.eachDelta(from+1, to, func(n *storage.CommitNotification) error {
			if !w.step(n.Commit, n.Patch, n.ScopeID, false) {
				return errWatchEnded
			}
			return nil
		})
		switch {
		case errors.Is(err, errWatchEnded):
			// Already failed, and already told the client why.
			return false
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
			// The coarse wake fires this watcher for every write under a shared top-level
			// subtree. The projection says cheaply whether the entry reaches this path at
			// all -- a plain merge that misses it says nothing about it -- and a scoped
			// watcher, which re-reads per event, is spared the read exactly then. The
			// watch is still correct through the commit.
			if at, _, ok := api.ProjectDelta(notification.Patch, w.path); ok && at == nil {
				w.accountFor(notification.Commit)
				continue
			}
			if !w.seeded && !w.seedAt(notification.Commit-1) {
				return
			}
			if !w.step(notification.Commit, notification.Patch, notification.ScopeID, true) {
				return
			}
		}
	}
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
	// What counts as a change is api.SameState's to say, here and in stepBaseline. See
	// it for why the answer counts comments.
	if api.SameState(newDoc, prev) {
		return prev, nil
	}
	// The delta carries what the equality counts, or the two disagree in the other
	// direction: a change SameState reports would be diffed away to nothing and the
	// watcher told a commit happened by a patch that changes nothing. Inert on a
	// document with no comments, like the equality above it.
	//
	// Absence is a side of the diff too, and it is stated as what it is: a value arriving
	// where there was none is an insert, a value leaving is a delete. Neither is a null,
	// which is a value (api/state.go). Both absent is the equality's case, above.
	// Rooted at the path, as the state event was and as a baseline delta is: one
	// rooting, and a client applies what arrives to what it holds.
	s.send(patchEvent(id, commit, path, deltaAt(prev, newDoc), newDoc == nil))
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
// rather than re-reading everything; 0 when it never got that far.
//
// failWatch ends a watch and says why: a reason code from the ErrCode* vocabulary, and
// the message that carries what the code cannot. The message goes to the CLIENT as well
// as the log -- it used to be logged and dropped, so a client could be told its cursor was
// compacted but not what the store still held (zmq8bdhwh12kstkqjhn0).
func (s *Session) failWatch(watcher *Watcher, reason, message string, commit int64) {
	s.log.Warn("watch ended", "path", watcher.Path, "reason", reason, "detail", message, "commit", commit)
	// Stamp the watch id so the client fails the right watch (several may share a path).
	s.send(api.NewEndedEvent(watcher.ID, watcher.Path, reason, message, commit))
	s.hub.Unwatch(watcher)
	s.watchMu.Lock()
	delete(s.watches, watchKey(watcher.ID, watcher.Path))
	s.watchMu.Unlock()
}

// absentAt answers the PathError when kp holds nothing at commit, and nil when it holds
// something -- which includes holding a null somebody wrote.
//
// It asks the same question sendInitialState asks, because the two must agree: one refuses
// the watch and the other seeds it, and a watch refused for a path the seed would have
// found is worse than either answer alone.
func (s *Session) absentAt(kp string, commit int64) *PathError {
	if commit == 0 {
		// Nothing has ever been written, so nothing is at kp -- any kp.
		return &PathError{Kind: PathAbsent, Path: kp}
	}
	_, err := s.readValueAt(kp, commit)
	var pe *PathError
	if errors.As(err, &pe) && pe.Kind == PathAbsent {
		return pe
	}
	// Anything else is not an answer about the path: let the watch establish and let
	// the seed report it, which is where a read failure has always been reported from.
	return nil
}

// eachDelta walks the commits in [from, to] that can reach the watched path, in the
// session's view, handing each to fn -- one entry in hand at a time (storage.Deltas).
func (w *watchStream) eachDelta(from, to int64, fn func(*storage.CommitNotification) error) error {
	cur, err := w.s.storage.Deltas(from, to, w.s.scopeID(), w.path)
	if err != nil {
		return err
	}
	defer cur.Close()
	for {
		n, err := cur.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := fn(n); err != nil {
			return err
		}
	}
}
