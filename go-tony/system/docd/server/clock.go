package server

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/docd/api"
	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// clock is a docd-driven virtual system clock served at Path. It retains no tick
// history: valueAtTick computes Epoch + N*Frequency on demand, where N is the
// number of whole Frequencies elapsed since start (the mount-establishment time).
// The result is a single monotonic, quantized int64. See api.ClockSpec.
type clock struct {
	path  string
	freq  time.Duration
	epoch int64
	start time.Time

	// now is time.Now in production; tests inject a deterministic source.
	now func() time.Time

	// gone is closed when the clock is unregistered, ending every watch on it: a
	// watch holds the clock itself, not its path, so without this it would go on
	// ticking a clock nobody serves any more.
	gone chan struct{}
}

// newClock builds a clock from a spec, validating the frequency. start is the
// tick-0 reference (when the clock mount was established).
func newClock(spec *api.ClockSpec, start time.Time) (*clock, error) {
	if spec.Path == "" {
		return nil, fmt.Errorf("clock path is required")
	}
	if isMetaPath(spec.Path) {
		return nil, fmt.Errorf("path .meta is reserved by docd")
	}
	if fields, err := claimedPathFields(spec.Path); err != nil || len(fields) == 0 {
		return nil, fmt.Errorf("clock path must be a non-empty kpath (no leading /)")
	}
	freq, err := time.ParseDuration(spec.Frequency)
	if err != nil {
		return nil, fmt.Errorf("invalid clock frequency %q: %w", spec.Frequency, err)
	}
	if freq <= 0 {
		return nil, fmt.Errorf("clock frequency must be positive, got %q", spec.Frequency)
	}
	return &clock{path: spec.Path, freq: freq, epoch: spec.Epoch, start: start, now: time.Now, gone: make(chan struct{})}, nil
}

// ticksAt returns the whole-tick count elapsed by t (0 before start).
func (c *clock) ticksAt(t time.Time) int64 {
	d := t.Sub(c.start)
	if d < 0 {
		return 0
	}
	return int64(d / c.freq)
}

// valueAtTick returns the value at tick n: Epoch + n*Frequency, Frequency counted
// in nanoseconds.
func (c *clock) valueAtTick(n int64) int64 {
	return c.epoch + n*c.freq.Nanoseconds()
}

// value returns the current clock value.
func (c *clock) value() int64 {
	return c.valueAtTick(c.ticksAt(c.now()))
}

// node renders the current value as an ir node (a single int64).
func (c *clock) node() *ir.Node {
	return ir.FromInt(c.value())
}

// clockRegistry tracks docd-driven virtual clocks by their exact serving path.
// A clock's lifetime is the mount connection that established it (see
// MountSession handshake/cleanup); this registry is the lookup for client reads.
type clockRegistry struct {
	mu     sync.RWMutex
	clocks map[string]*clock
}

func newClockRegistry() *clockRegistry {
	return &clockRegistry{clocks: make(map[string]*clock)}
}

// register adds c, rejecting a path already held by another clock.
func (r *clockRegistry) register(c *clock) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.clocks[c.path]; exists {
		return fmt.Errorf("clock path %q already in use", c.path)
	}
	r.clocks[c.path] = c
	return nil
}

// unregister removes the clock at path only if it is still the one owned by this
// caller (guards against a re-registered path being dropped by a late cleanup), and
// ends the watches on it.
func (r *clockRegistry) unregister(c *clock) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.clocks[c.path] == c {
		delete(r.clocks, c.path)
		close(c.gone)
	}
}

// lookup returns the clock serving exactly path, or nil.
func (r *clockRegistry) lookup(path string) *clock {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.clocks[path]
}

// list returns all registered clocks, sorted by path.
func (r *clockRegistry) list() []*clock {
	r.mu.RLock()
	out := make([]*clock, 0, len(r.clocks))
	for _, c := range r.clocks {
		out = append(out, c)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

// clocksDoc renders the clock registry as a Tony document for .meta/clocks:
//
//	clocks:
//	- path: sys/clock
//	  frequency: 1s
//	  epoch: 0
//
// Epoch and frequency are recorded here so a consumer can recover them without
// replaying ticks (the clock itself serves only the current value).
func clocksDoc(clocks []*clock) *ir.Node {
	items := make([]*ir.Node, 0, len(clocks))
	for _, c := range clocks {
		items = append(items, ir.FromKeyVals([]ir.KeyVal{
			{Key: ir.FromString("path"), Val: ir.FromString(c.path)},
			{Key: ir.FromString("frequency"), Val: ir.FromString(c.freq.String())},
			{Key: ir.FromString("epoch"), Val: ir.FromInt(c.epoch)},
		}))
	}
	return ir.FromKeyVals([]ir.KeyVal{
		{Key: ir.FromString("clocks"), Val: ir.FromSlice(items)},
	})
}

// clockWatcher is a live clock watch: a ticker goroutine emits a state event with
// the clock's value every Frequency until stop is closed (unwatch or session
// close) or the clock is unregistered. clientID is stamped on every event so the
// client routes it to the right watch (several may share a connection).
//
// A clock is not in the commit sequence, so its events carry commit 0, as its
// match result does. The value is the state, never the commit: a commit stamped on
// an event raises the mark docd reports on a pong and the client's KnownCommit.
type clockWatcher struct {
	clientID *string
	stop     chan struct{}
}

// clockFor returns the clock serving the request's path, or nil when the path is
// not a clock. Only Match/Watch/Unwatch carry a path; other ops return nil.
func (s *ClientSession) clockFor(req *logdapi.SessionRequest) *clock {
	path := requestPath(req)
	if path == "" {
		return nil
	}
	return s.server.Clocks.lookup(path)
}

// serveClockMatch answers a MATCH on a clock path with the clock's current value,
// mirroring serveMeta's direct-from-docd response shape.
func (s *ClientSession) serveClockMatch(req *logdapi.SessionRequest, clk *clock) {
	_ = s.writeToClient(&logdapi.SessionResponse{
		ID:     req.ID,
		Result: &logdapi.SessionResult{Match: &logdapi.MatchResult{Body: clk.node()}},
	})
}

// serveClockWatch establishes a clock watch: it sends the current value as the
// initial state, a replay-complete marker, then a fresh state event on every tick
// until the watch is dropped. When the clock's mount connection closes, the watch
// ends with session_unmounted: the clock is removed outright, so a re-watch of the
// path no longer finds it. Clocks need no mount coordination (no controller, no
// draining), so they are served directly rather than through the coordinator.
func (s *ClientSession) serveClockWatch(req *logdapi.SessionRequest, clk *clock) {
	clientID := req.ID
	key := watchKeyFor(clientID, clk.path)

	w := &clockWatcher{clientID: clientID, stop: make(chan struct{})}

	s.watchMu.Lock()
	if s.closing {
		s.watchMu.Unlock()
		return
	}
	if old := s.clockWatches[key]; old != nil {
		close(old.stop) // a same-key re-watch replaces the previous one
	}
	s.clockWatches[key] = w
	s.watchMu.Unlock()

	// Initial snapshot + replay-complete, unless the client asked to skip init.
	if req.Watch == nil || !req.Watch.NoInit {
		if err := s.writeToClient(logdapi.NewStateEvent(clientID, 0, clk.path, clk.node())); err != nil {
			s.stopClockWatch(key)
			return
		}
		if err := s.writeToClient(logdapi.NewReplayCompleteEvent(clientID, clk.path)); err != nil {
			s.stopClockWatch(key)
			return
		}
	}

	go func() {
		ticker := time.NewTicker(clk.freq)
		defer ticker.Stop()
		for {
			select {
			case <-w.stop:
				return
			case <-s.done:
				return
			case <-clk.gone:
				s.endClockWatch(key, w, clk.path)
				return
			case <-ticker.C:
				if err := s.writeToClient(logdapi.NewStateEvent(clientID, 0, clk.path, clk.node())); err != nil {
					return
				}
			}
		}
	}()
}

// endClockWatch ends watch w on path, held under key, because its clock was
// unregistered. The terminal event is written only if w was still the live watch
// there: one an unwatch, a re-watch or session close already dropped has nothing
// to tell.
func (s *ClientSession) endClockWatch(key string, w *clockWatcher, path string) {
	s.watchMu.Lock()
	live := s.clockWatches[key] == w
	if live {
		delete(s.clockWatches, key)
	}
	s.watchMu.Unlock()
	if !live {
		return
	}
	_ = s.writeToClient(terminalWatchEvent(w.clientID, path, logdapi.ErrCodeSessionUnmounted,
		"clock unmounted", 0))
}

// stopClockWatch stops and forgets the clock watch with the given key. Safe when
// none is held (an unwatch of a path this session was not watching).
func (s *ClientSession) stopClockWatch(key string) {
	s.watchMu.Lock()
	w, ok := s.clockWatches[key]
	if ok {
		delete(s.clockWatches, key)
	}
	s.watchMu.Unlock()
	if ok {
		close(w.stop)
	}
}

// stopAllClockWatches stops every clock watch the session holds (called on
// teardown alongside releaseAllWatches).
func (s *ClientSession) stopAllClockWatches() {
	s.watchMu.Lock()
	ws := make([]*clockWatcher, 0, len(s.clockWatches))
	for _, w := range s.clockWatches {
		ws = append(ws, w)
	}
	s.clockWatches = make(map[string]*clockWatcher)
	s.watchMu.Unlock()
	for _, w := range ws {
		close(w.stop)
	}
}
