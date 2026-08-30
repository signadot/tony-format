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

// unwatchTimeout bounds the unwatch a client sends when it stops reading a watch,
// whether by Close or by abandoning one it was still establishing. It is a courtesy
// to the server, so it is bounded and its failure is not the caller's problem.
const unwatchTimeout = 5 * time.Second

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
// Mounts share the commit sequence for their lifetime, so a commit is an exact resume
// point across a composed path as well as a single-route one. What a watcher must account
// for is membership: a mount arriving or leaving mid-watch ends the watch with one of the
// reasons above, and the re-watch composes the new membership.
//
// One gap: docd does not yet pass WatchOptions.FromCommit down to a composed watch's
// sub-watches, so re-watching a composed path re-inits with a fresh State snapshot rather
// than replaying (issue 4ses3fqsh12ks8awgnn0). A snapshot-diffing consumer needs no resume
// point either way.
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
	//
	// A NEGATIVE value is relative: -N asks for the last N commits, resolved by the
	// server against its watermark when the watch is established, and clamped to the
	// retained history rather than refused. It is how a caller asks for a window of
	// history without knowing where the store is -- Watch.ReplayingFrom reports which
	// commit it actually resolved to.
	FromCommit *int64

	// NoInit skips the initial full-state event. By default the first event
	// carries the full state at the starting commit.
	NoInit bool

	// WaitIfAbsent asks to watch a path that holds nothing yet, and to be told when a
	// value arrives. Without it such a watch is refused with not_found, which is what a
	// read of the same path answers.
	WaitIfAbsent bool

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

	// replayingFrom/replayingTo are the confirmed replay range, nil when the watch is
	// not replaying. See ReplayingFrom.
	replayingFrom *int64
	replayingTo   *int64
	events        chan *api.WatchEvent

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

	if err := s.ensureConnected(ctx); err != nil {
		return nil, err
	}
	if err := s.acquireWire(ctx); err != nil {
		return nil, err
	}

	s.mu.Lock()
	conn := s.conn
	if conn == nil {
		s.mu.Unlock()
		s.releaseWire()
		return nil, s.connError()
	}
	id := s.newIDLocked()
	w.id = id

	// Register the watcher BEFORE sending, so the initial state event (which
	// logd may push immediately) is routed to us rather than dropped. Keyed by the
	// watch's request id so multiple watches on one path stay distinct.
	s.watchers[id] = w

	replyCh := make(chan *api.SessionResponse, 1)
	s.pending[id] = replyCh
	s.mu.Unlock()

	req := &api.SessionRequest{
		ID: &id,
		Watch: &api.WatchRequest{
			Path:         path,
			FromCommit:   opts.FromCommit,
			NoInit:       opts.NoInit,
			WaitIfAbsent: opts.WaitIfAbsent,
		},
	}
	err := s.sendRequestTo(conn, req)
	s.releaseWire()
	if err != nil {
		s.mu.Lock()
		delete(s.pending, id)
		delete(s.watchers, id)
		s.mu.Unlock()
		s.failConn(conn, err)
		return nil, err
	}

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
		w.replayingFrom = resp.Result.Watch.ReplayingFrom
		w.replayingTo = resp.Result.Watch.ReplayingTo
		return w, nil
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pending, id)
		delete(s.watchers, id)
		s.mu.Unlock()
		// The request is already on the wire, so logd may well have registered this
		// watch — and the session stays up on the same connection, so from the
		// server's side this is a healthy client with a watch it does not read.
		// Nothing will ever tell it otherwise, and every commit thereafter fans out
		// to a watcher no one will receive from. Only the client knows, so it says
		// so on the way out.
		go s.unwatchAbandoned(path, id)
		return nil, ctx.Err()
	case <-s.done:
		return nil, fmt.Errorf("session closed")
	}
}

// ReplayingFrom is the commit this watch replays from, and nil when it is not
// replaying at all. A caller which asked for a relative window (a negative
// WatchOptions.FromCommit) learns here which commits it is getting -- and a caller
// whose request was clamped to the retained history, or dropped by a composed watch
// which cannot honour a cursor, can see that too.
func (w *Watch) ReplayingFrom() *int64 { return w.replayingFrom }

// ReplayingTo is the commit the replay runs up to, and nil when the watch is not
// replaying.
func (w *Watch) ReplayingTo() *int64 { return w.replayingTo }

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
	ctx, cancel := context.WithTimeout(context.Background(), unwatchTimeout)
	defer cancel()
	return w.session.unwatch(ctx, w.path, w.id)
}

// unwatchAbandoned tells logd to drop a watch whose caller gave up while the watch
// request was in flight. It runs off the caller's goroutine and on a context of its
// own: the caller's context is what expired, and the caller is not waiting for this.
// Best-effort — if the connection is gone the server has already dropped the watch
// with the session.
func (s *LogdSession) unwatchAbandoned(path, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), unwatchTimeout)
	defer cancel()
	if err := s.unwatch(ctx, path, id); err != nil {
		s.log.Debug("unwatch of abandoned watch failed",
			"path", path, "watchID", id, "error", err)
	}
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
