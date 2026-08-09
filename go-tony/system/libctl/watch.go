package libctl

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// defaultWatchBuffer is the event channel capacity used when WatchOptions does
// not specify one.
const defaultWatchBuffer = 128

// WatchEndedError terminates a Watch when docd ends it server-side rather than
// the connection dropping. Reason says which of these happened, from the
// api.ErrCode* vocabulary:
//
//	session_mounted         a mount registered at or under the watched path, so a
//	                        re-watch composes over a source that was not there
//	session_unmounted       a mount at or under it was removed, so a source that
//	                        was answering is gone
//	controller_unavailable  the controller owning the watched subtree crashed
//	match_failed            the composed read backing the watch failed, so there
//	                        is no baseline to stream deltas against
//
// The watch is re-establishable in every case: the application should start a new
// Watch on the same path, which re-composes against the current mount set. The
// reason is for a caller that does more than reconnect — one deciding whether the
// content it is about to see should differ, or reporting why the stream broke.
//
// Event-preservation is a logd guarantee that docd inherits fully only for a
// single-route watch (a path served by one backend): re-watching with
// WatchOptions.FromCommit set to Commit replays the exact delta history from Commit
// with no loss. A COMPOSED watch (an ancestor path spanning several mounts) multiplexes
// backends with independent commit sequences, so across a mount membership change
// docd's event-preservation is best-effort: the re-watch re-inits with a fresh State
// snapshot (a re-sync to current) rather than replaying the gap, conveying net state
// rather than the intermediate deltas. Commit is thus an exact resume point for a
// single-route watch and a best-effort hint for a composed one — a snapshot-diffing
// consumer needs neither and can just re-watch and reconcile against its last snapshot.
type WatchEndedError struct {
	Path   string
	Reason string
	Commit int64
}

func (e *WatchEndedError) Error() string {
	return fmt.Sprintf("watch on %q ended at commit %d: %s", e.Path, e.Commit, e.Reason)
}

// WatchOptions configures a Watch subscription.
type WatchOptions struct {
	// FromCommit, when non-nil, replays historical patches after this commit up
	// to the current commit before streaming live events. When nil, the watch
	// starts from the current state.
	FromCommit *int64

	// NoInit skips the initial full-state event. By default the first event
	// carries the full state at the starting commit.
	NoInit bool

	// BufferSize sets the capacity of the event channel. Defaults to 128. If a
	// consumer falls far enough behind to fill the buffer, the watch is failed
	// as a slow consumer (mirroring logd's own server-side behavior) rather
	// than stalling the shared connection.
	BufferSize int
}

// Watch is an active subscription to changes at a path in logd.
//
// Events are delivered on Events() in commit order. The channel is closed when
// the watch ends — because Close was called, the connection failed, or the
// consumer fell too far behind. After the channel closes, Err reports the
// cause (nil for a clean Close).
//
// A Watch shares the LogdSession's connection with all other requests and
// watches. Because it is bound to that connection, a Watch does not survive a
// reconnect: if the connection drops, the watch fails and the caller must
// re-establish it.
type Watch struct {
	id      string
	path    string
	session *LogdSession
	events  chan *api.WatchEvent

	mu     sync.Mutex
	closed bool
	err    error
}

// Watch starts watching changes at path. It registers the watch, sends the
// watch request, and waits for logd's confirmation before returning. Events
// then stream on the returned Watch's Events() channel until it is closed.
//
// A session may hold several watches on the same path; each is identified by its
// own request id, and events are routed by that id.
func (s *LogdSession) Watch(ctx context.Context, path string, opts *WatchOptions) (*Watch, error) {
	if opts == nil {
		opts = &WatchOptions{}
	}
	bufSize := opts.BufferSize
	if bufSize <= 0 {
		bufSize = defaultWatchBuffer
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, fmt.Errorf("session closed")
	default:
	}

	w := &Watch{
		path:    path,
		session: s,
		events:  make(chan *api.WatchEvent, bufSize),
	}

	s.mu.Lock()
	if err := s.ensureConnected(ctx); err != nil {
		s.mu.Unlock()
		return nil, err
	}

	id := s.newIDLocked()
	w.id = id

	// Register the watcher BEFORE sending, so the initial state event (which
	// logd may push immediately) is routed to us rather than dropped. Keyed by the
	// watch's request id so multiple watches on one path stay distinct.
	s.watchers[id] = w

	replyCh := make(chan *api.SessionResponse, 1)
	s.pending[id] = replyCh

	req := &api.SessionRequest{
		ID: &id,
		Watch: &api.WatchRequest{
			Path:       path,
			FromCommit: opts.FromCommit,
			NoInit:     opts.NoInit,
		},
	}
	if err := s.sendRequestTo(s.conn, req); err != nil {
		delete(s.pending, id)
		delete(s.watchers, id)
		conn, pending, watchers := s.teardownLocked(err)
		s.mu.Unlock()
		releaseResources(conn, pending, watchers, err)
		return nil, err
	}
	s.mu.Unlock()

	select {
	case resp, ok := <-replyCh:
		if !ok {
			return nil, s.connError()
		}
		if resp.Error != nil {
			s.removeWatcher(id)
			return nil, fmt.Errorf("watch error: %w", resp.Error)
		}
		if resp.Result == nil || resp.Result.Watch == nil {
			s.removeWatcher(id)
			return nil, fmt.Errorf("unexpected response: no watch result")
		}
		return w, nil
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pending, id)
		delete(s.watchers, id)
		s.mu.Unlock()
		return nil, ctx.Err()
	case <-s.done:
		return nil, fmt.Errorf("session closed")
	}
}

// Events returns the channel of streaming watch events. It is closed when the
// watch ends; check Err afterwards for the cause.
func (w *Watch) Events() <-chan *api.WatchEvent {
	return w.events
}

// Path returns the watched path.
func (w *Watch) Path() string {
	return w.path
}

// Err returns the error that terminated the watch, or nil if it was closed
// cleanly (or is still active).
func (w *Watch) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

// Close stops the watch and releases it. It unregisters the watch locally and
// best-effort sends an unwatch to logd so the server stops forwarding events.
func (w *Watch) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	close(w.events)
	w.mu.Unlock()

	w.session.removeWatcher(w.id)

	// Tell logd to stop the watch. Bounded so Close can't hang.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return w.session.unwatch(ctx, w.path, w.id)
}

// deliver hands an event to the consumer. It never blocks the read-pump: if the
// consumer has fallen behind enough to fill the buffer, the watch is failed as
// a slow consumer instead.
func (w *Watch) deliver(ev *api.WatchEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	select {
	case w.events <- ev:
	default:
		w.err = fmt.Errorf("watch on %q failed: slow consumer", w.path)
		w.closed = true
		close(w.events)
	}
}

// fail terminates the watch with an error (e.g. the connection dropped).
func (w *Watch) fail(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if err != nil {
		w.err = err
	}
	w.closed = true
	close(w.events)
}

// unwatch sends an unwatch request targeting the specific watch id on path. It is
// best-effort: if the session is not currently connected, there is nothing to
// unwatch.
func (s *LogdSession) unwatch(ctx context.Context, path, watchID string) error {
	s.mu.Lock()
	connected := s.connected
	s.mu.Unlock()
	if !connected {
		return nil
	}

	resp, err := s.request(ctx, &api.SessionRequest{
		Unwatch: &api.UnwatchRequest{Path: path, WatchID: &watchID},
	})
	if err != nil {
		return err
	}
	if resp.Error != nil {
		// not_watching means the server already dropped it; not an error worth
		// surfacing on Close.
		if resp.Error.Code == api.ErrCodeNotWatching {
			return nil
		}
		return fmt.Errorf("unwatch error: %w", resp.Error)
	}
	return nil
}
