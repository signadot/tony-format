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
func (s *Session) forwardEvents(watcher *Watcher, fromCommit *int64, noInit bool, currentCommit int64) {
	path := watcher.Path
	scoped := s.scopeID() != nil

	// Track the highest commit we've replayed (for deduplication)
	lastReplayedCommit := int64(0)

	// The highest commit this watch has accounted for, handed to the client on a terminal
	// event as its resume point. It advances even for a commit that produced no event for
	// this path — the watch is correct through that commit, so resuming above it skips
	// replaying history the client already has. Zero until the watch has caught up to
	// anything, which is what a client that never got started should resume from.
	lastDelivered := int64(0)

	// Determine the starting commit for initial state
	startCommit := currentCommit
	if fromCommit != nil {
		startCommit = *fromCommit
		lastReplayedCommit = currentCommit
	}

	// Refuse a cursor below the retained delta window before sending ANYTHING. The
	// replay itself would catch this (ReadPatchesInRange returns ErrReplayCompacted), but
	// only after the initial state has gone out — and a state read below the floor is
	// itself approximate, since compaction leaves historical reads at snapshot
	// granularity. Handing the client a state it cannot trust and then an error is worse
	// than the error alone.
	//
	// The bound matches the replay's: it reads [startCommit+1, ...], so a cursor AT the
	// floor is fine — "I have through commit F" needs only the deltas above F, which are
	// intact — and one below it is not.
	if fromCommit != nil {
		if floor := s.storage.ReplayFloor(); *fromCommit < floor {
			s.log.Warn("watch cursor below retained history", "path", path, "fromCommit", *fromCommit, "floor", floor)
			s.failWatch(watcher, api.ErrCodeReplayCompacted, fmt.Sprintf(
				"cannot replay from commit %d: delta history is retained only from commit %d; re-watch without fromCommit to re-initialize",
				*fromCommit, floor+1), 0)
			return
		}
	}

	// A watch whose path has no value yet is the ordinary way to start watching
	// something that does not exist. Say it once, quietly, and say so again when
	// it arrives: the pair is a story an operator can follow, where the same line
	// repeated per event is just an alarm they learn to ignore.
	absent := &watchAbsence{log: s.log, path: path}

	// Send initial state unless noInit is set
	if !noInit {
		var state *ir.Node
		if startCommit == 0 {
			// Empty store - state is null
			state = ir.Null()
		} else {
			var err error
			state, err = s.readDocAt(path, startCommit)
			if err != nil {
				s.log.Error("failed to read state for init", "path", path, "commit", startCommit, "error", err)
				s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read state at commit %d: %v", startCommit, err), lastDelivered)
				return
			}
			// Extract value at path if needed.
			//
			// A failure here says nothing about storage: the read above already
			// succeeded, and this is navigation of the document it returned. What
			// it says is which of three things is true, and they want three
			// different volumes -- see PathErrorKind.
			if path != "" {
				state, err = extractPathValue(state, path)
				if err != nil {
					var pe *PathError
					switch {
					case errors.As(err, &pe) && pe.Kind == PathBadSegment:
						// This one never resolves, so serving null forever would
						// tell the client its path is empty when it is invalid.
						s.log.Warn("watch path cannot be extracted", "path", path, "error", err)
						s.failWatch(watcher, api.ErrCodeInvalidPath, err.Error(), lastDelivered)
						return
					case errors.As(err, &pe) && pe.Kind == PathTypeConflict:
						s.log.Warn("watched path is shadowed by a non-object", "path", path, "error", err)
					default:
						s.log.Debug("watched path has no value yet", "path", path, "detail", err.Error())
					}
					state = ir.Null()
					absent.arm()
				}
			}
		}
		s.send(api.NewStateEvent(watcher.ID, startCommit, path, state))
		lastDelivered = startCommit
	}

	// prevDoc tracks the watched path's own subtree (as scopedDocAt trims it) at the
	// last delivered commit. A scoped watcher uses it to emit deltas by
	// recompute-and-diff (it must reproduce the scoped read at each commit; a raw
	// committed delta does not, because scope writes shadow baseline and !key merges
	// are identity-based). A BASELINE watcher uses it only as a change GATE: a coarse
	// wake (top-level KPath) plus the superset read can wake a watcher for a commit
	// that only touched a sibling under a shared ancestor, so before forwarding the
	// raw committed delta (which baseline keeps for op fidelity) we confirm the
	// watcher's own subtree actually changed. See issue eagjggjdh12ksg00bsn0.
	//
	// prevDoc is seeded at startCommit for a fromCommit replay (the replay below diffs
	// or gates forward from it). For a no-replay watcher it is seeded LAZILY, at
	// (firstEvent.commit - 1) when the first event arrives — never at startCommit —
	// because a write can race into [hub-register, GetCurrentCommit] and be queued with
	// commit <= startCommit; seeding at startCommit would fold that write into the
	// baseline and drop its delta (the scoped-watch drop-one-event regression). Lazy
	// seeding makes every queued or live event a correct forward diff.
	//
	// A BASELINE watcher additionally keeps curDoc, the whole document at that same
	// commit, and STEPS it: curDoc = Patch(curDoc, committedPatch), then trims to get the
	// subtree. That is the read path's own fold (processor.go applies patches with
	// tony.Patch, and read_equivalence_test's oracle calls the fold from commit 0 "the
	// semantics of record"), just not restarted from a snapshot every time. It replaces a
	// full ReadStateAt per event per watcher, which was O(patches since the last
	// snapshot): 1.6ms at 50 commits, 62ms at 1550, paid again by every watcher on every
	// commit that reached it. Nothing is kept that was not already built — the old code
	// materialized a whole document per event and threw it away.
	//
	// A SCOPED watcher cannot step: its view is baseline with the scope's own writes
	// applied LAST, so they shadow baseline stickily, and applying a baseline patch to a
	// materialized scoped document would let a baseline write overwrite a leaf the scope
	// owns. It keeps recompute-and-diff until the scope layer gets the same treatment.
	var prevDoc *ir.Node
	var curDoc *ir.Node
	// A scoped watcher's stepper, seeded with prevDoc. nil means recompute per event, which
	// is what every scoped watcher did before and what a scope the overlay cannot serve
	// still does.
	var stepper *storage.ScopedWatchStepper
	prevSeeded := false
	if fromCommit != nil {
		var err error
		if scoped {
			prevDoc, err = s.scopedDocAt(path, startCommit)
		} else {
			curDoc, err = s.fullDocAt(startCommit)
			prevDoc = subtreeOf(curDoc, path)
		}
		if err != nil {
			s.log.Error("failed to read watch base", "path", path, "commit", startCommit, "error", err)
			s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read state at commit %d: %v", startCommit, err), lastDelivered)
			return
		}
		prevSeeded = true
	}

	// Handle replay if fromCommit is specified
	if fromCommit != nil {
		// Send historical patches from startCommit+1 to currentCommit
		if startCommit < currentCommit {
			patches, err := s.storage.ReadPatchesInRange(path, startCommit+1, currentCommit, s.scopeID())
			if errors.Is(err, storage.ErrReplayCompacted) {
				// The cursor predates the retained delta window (compaction cutoff), so
				// the exact history it asked for no longer exists. Say that specifically:
				// a client told "replay_compacted" re-watches without fromCommit and
				// re-initializes from current state, where "replay_failed" reads as a
				// transient fault worth retrying with the same doomed cursor.
				s.log.Warn("watch replay below retained history", "path", path, "fromCommit", startCommit, "error", err)
				s.failWatch(watcher, api.ErrCodeReplayCompacted,
					fmt.Sprintf("cannot replay from commit %d: %v; re-watch without fromCommit to re-initialize", startCommit, err), lastDelivered)
				return
			}
			if err != nil {
				s.log.Error("failed to read patches for replay", "path", path, "from", startCommit+1, "to", currentCommit, "error", err)
				s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read patches from commit %d to %d: %v", startCommit+1, currentCommit, err), lastDelivered)
				return
			}
			for _, patch := range patches {
				if scoped {
					newPrev, err := s.emitScopedDelta(watcher.ID, path, patch.Commit, prevDoc)
					if err != nil {
						s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read scoped state at commit %d: %v", patch.Commit, err), lastDelivered)
						return
					}
					prevDoc = newPrev
					absent.observe(prevDoc)
					lastDelivered = patch.Commit
					continue
				}
				// Baseline: forward the raw delta (op fidelity), but only if this
				// watcher's own subtree actually changed at this commit — the range
				// read can include a sibling's write under a shared ancestor.
				//
				// Step rather than re-read. The tags are stripped first because they are
				// the streaming processor's patch-root markers, not part of the value; the
				// send below strips for the same reason.
				tx.StripPatchRootTagRecursive(patch.Patch)
				stepped, err := api.NextState(curDoc, patch.Patch)
				if err != nil {
					s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to apply patch at commit %d: %v", patch.Commit, err), lastDelivered)
					return
				}
				curDoc = stepped
				newSub := subtreeOf(curDoc, path)
				// Accounted for either way: a commit that changed nothing under this path
				// still leaves the watch correct through it, so it is a valid resume point.
				lastDelivered = patch.Commit
				// api.SameState decides what counts as a change; see it for comments.
				if api.SameState(newSub, prevDoc) {
					continue
				}
				prevDoc = newSub
				absent.observe(prevDoc)
				s.send(api.NewPatchEvent(watcher.ID, patch.Commit, path, patch.Patch))
			}
		}

		s.send(api.NewReplayCompleteEvent(watcher.ID, path))
	}

	// Forward live events, skipping any already replayed
	for {
		select {
		case <-s.done:
			return
		case <-watcher.Failed:
			// Broadcast dropped this watcher because its buffer was full. Report it as
			// what it is: the client fell behind, and it can resume from lastDelivered
			// rather than re-reading the whole document.
			s.failWatch(watcher, api.ErrCodeSlowConsumer,
				fmt.Sprintf("watch on %q dropped: consumer did not keep up", path), lastDelivered)
			return
		case notification, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Skip events that were already replayed (deduplication for race prevention)
			if notification.Commit <= lastReplayedCommit {
				continue
			}
			// Cheap pre-filter: the coarse wake fires this watcher for every write
			// under a shared top-level subtree, but recomputing full state per event
			// is O(log) and collapses under many watches. If the committed delta
			// clearly cannot reach this watcher's subtree (a plain merge that misses
			// the path), skip the recompute. Conservative: any op tag or a
			// non-navigable ancestor falls through to the authoritative recompute.
			if !patchMayAffect(notification.Patch, path) {
				// The watch is correct through this commit -- the delta cannot reach
				// its path -- so it is accounted for. Not advancing here left a resume
				// point older than the truth, which costs a re-watch the replay of
				// everything since.
				lastDelivered = notification.Commit
				continue
			}
			if scoped {
				// Lazily seed the diff baseline at the commit just before the first
				// event, so a queued race-window event (commit <= startCommit) yields a
				// correct forward delta instead of being folded into the baseline.
				if !prevSeeded {
					var err error
					prevDoc, err = s.scopedDocAt(path, notification.Commit-1)
					if err != nil {
						s.log.Error("failed to read scoped watch base", "path", path, "commit", notification.Commit-1, "error", err)
						s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read scoped state at commit %d: %v", notification.Commit-1, err), lastDelivered)
						return
					}
					prevSeeded = true

					// Seed a stepper at the same commit. From here the scoped view is
					// derived by folding each event into a baseline document the watcher
					// keeps -- the same move a baseline watcher already makes -- instead
					// of recomputing the whole view per event. It returns nil when the
					// scope cannot be served that way, and then nothing below changes.
					stepper, err = s.storage.NewScopedWatchStepper(*s.scopeID(), notification.Commit-1)
					if err != nil {
						s.log.Warn("scoped watch stepper unavailable; recomputing per event",
							"path", path, "error", err)
						stepper = nil
					}
				}
				// Recompute the scoped view at this commit and emit only the change
				// vs. the previously emitted state. notification.Patch (the raw
				// baseline/scope delta) is intentionally ignored -- except by the
				// stepper, which folds it.
				newPrev, err := s.emitScopedDeltaStepped(watcher.ID, path, notification.Commit, prevDoc, stepper, notification)
				if err != nil {
					s.log.Error("failed to read scoped state for watch", "path", path, "commit", notification.Commit, "error", err)
					s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read scoped state at commit %d: %v", notification.Commit, err), lastDelivered)
					return
				}
				prevDoc = newPrev
				absent.observe(prevDoc)
				// Accounted for, as the baseline path does below and as the replay
				// above does. Without this a SCOPED watch never advanced its resume
				// point while streaming live, so a watch dropped after an hour handed
				// the client the commit it started at -- and the re-watch either
				// replayed the hour or was refused as replay_compacted.
				lastDelivered = notification.Commit
				continue
			}
			// Baseline: forward the raw committed delta (already tag-stripped by the
			// hub, so read-only) to preserve op fidelity (!key etc.), but only if this
			// watcher's own subtree actually changed. A coarse wake plus the superset
			// read can deliver a commit that only touched a sibling under a shared
			// ancestor; the gate suppresses it. prevDoc is seeded lazily as scoped does.
			if !prevSeeded {
				var err error
				curDoc, err = s.fullDocAt(notification.Commit - 1)
				if err != nil {
					s.log.Error("failed to read watch base", "path", path, "commit", notification.Commit-1, "error", err)
					s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to read state at commit %d: %v", notification.Commit-1, err), lastDelivered)
					return
				}
				prevDoc = subtreeOf(curDoc, path)
				prevSeeded = true
			}
			// Step the document by this commit's delta instead of rebuilding it from the
			// last snapshot. notification.Patch is already the tick's private, stripped
			// copy, so it can be applied as-is and is not mutated by Patch.
			stepped, err := api.NextState(curDoc, notification.Patch)
			if err != nil {
				s.log.Error("failed to apply patch for watch", "path", path, "commit", notification.Commit, "error", err)
				s.failWatch(watcher, api.ErrCodeReplayFailed, fmt.Sprintf("failed to apply patch at commit %d: %v", notification.Commit, err), lastDelivered)
				return
			}
			curDoc = stepped
			newSub := subtreeOf(curDoc, path)
			// Accounted for whether or not it changed anything here (see the replay loop).
			lastDelivered = notification.Commit
			// api.SameState decides what counts as a change; see it for comments.
			if api.SameState(newSub, prevDoc) {
				continue
			}
			prevDoc = newSub
			absent.observe(prevDoc)
			// The hub broadcasts ONE shared notification.Patch to every watcher on the
			// path (across sessions); encoding mutates a node's parent linkage
			// (ir.FromMap), so two session writers serializing the same node race. Hand
			// this watcher its own copy — the shared node is then only ever read.
			s.send(api.NewPatchEvent(watcher.ID, notification.Commit, path, notification.Patch.DeepCopy()))
		}
	}
}

// emitScopedDelta recomputes the scoped view of the watched path's subtree at
// commit and, if it differs from prev, sends a root-rooted delta (prev -> new) as a
// patch event. prev and the returned value are the path's own subtree (as
// scopedDocAt trims it), so the diff is taken at the path and then re-rooted to the
// document root for the watch delta contract. It returns the new subtree to use as
// the next diff base (unchanged prev when there is no delta, so a baseline write to
// a scope-overridden leaf — or any sibling write — emits nothing).
// emitScopedDeltaStepped is emitScopedDelta with the option of a stepper. With one, the
// scope's document is folded from the committed patch rather than read back; without one,
// this is exactly the old path.
//
// The delta is still recompute-and-diff against the previous emitted state: a scope's raw
// committed patch is not its delta, because scope writes shadow baseline stickily and
// !key merges are identity-based. What the stepper removes is the READ, not the diff.
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
