package libctl

import (
	"context"
	"testing"
	"time"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// watchableLogdController is a logd-backed controller that ALSO serves watches, by
// proxying a logd watch stream in the client's scope. This is the scope-aware,
// watchable mount shape a real controller (e.g. verse's trigger) has — which the
// plain logdController (Watch -> ErrUnsupported) does not cover, leaving scoped
// watches under a mount untested.
type watchableLogdController struct {
	*logdController
}

func newWatchableLogdController(t *testing.T, logdAddr, id string) *watchableLogdController {
	return &watchableLogdController{logdController: newLogdController(t, logdAddr, id)}
}

func (c *watchableLogdController) Watch(ctx context.Context, path string, opts WatchParams, emit func(*api.WatchEvent) error) error {
	w, err := c.session(opts.Scope).Watch(ctx, path, &WatchOptions{NoInit: opts.NoInit, FromCommit: opts.FromCommit})
	if err != nil {
		return err
	}
	defer w.Close()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-w.Events():
			if !ok {
				return w.Err()
			}
			if err := emit(ev); err != nil {
				return err
			}
		}
	}
}

// TestVerse_ScopedSiblingWatchesUnderMount: a scoped client holds several watches
// on non-overlapping sibling leaves under one mount; each must get its own delta
// and no others.
func TestVerse_ScopedSiblingWatchesUnderMount(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "m", newWatchableLogdController(t, logd.TCPAddr(), "cM"))

	scoped := scopedDocdClient(t, docd, "scoped", "s1")
	ctx := context.Background()

	paths := []string{"m.a", "m.b", "m.c", "m.d"}
	watches := map[string]*Watch{}
	for _, p := range paths {
		w, err := scoped.Watch(ctx, p, nil)
		if err != nil {
			t.Fatalf("watch %s: %v", p, err)
		}
		t.Cleanup(func() { w.Close() })
		watches[p] = w
		if init := expectEvent(t, w); init.State == nil {
			t.Fatalf("watch %s: expected initial State, got %+v", p, init)
		}
	}

	// Write only m.a. Only the m.a watch should fire.
	if err := scoped.Patch(ctx, "m.a", vObj(1)); err != nil {
		t.Fatalf("scoped patch m.a: %v", err)
	}
	if ev := expectEvent(t, watches["m.a"]); ev.Patch == nil {
		t.Errorf("watch m.a: expected a delta Patch, got %+v", ev)
	}
	for _, p := range []string{"m.b", "m.c", "m.d"} {
		expectQuiet(t, p, watches[p])
	}
}

// TestVerse_ScopedOverlappingWatchesUnderMount reproduces the verse shape: a
// scoped client watches a path AND a subtree under it (overlapping "sibling
// subtrees"), plus a disjoint sibling, all under one mount. A write to the nested
// path must fire BOTH the ancestor and nested watch; a write to the disjoint
// sibling must fire only its own.
func TestVerse_ScopedOverlappingWatchesUnderMount(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "m", newWatchableLogdController(t, logd.TCPAddr(), "cM"))

	scoped := scopedDocdClient(t, docd, "scoped", "s1")
	ctx := context.Background()

	open := func(p string) *Watch {
		w, err := scoped.Watch(ctx, p, nil)
		if err != nil {
			t.Fatalf("watch %s: %v", p, err)
		}
		t.Cleanup(func() { w.Close() })
		if init := expectEvent(t, w); init.State == nil {
			t.Fatalf("watch %s: expected initial State, got %+v", p, init)
		}
		return w
	}

	wA := open("m.a")    // ancestor
	wAX := open("m.a.x") // nested subtree of m.a (overlap)
	wB := open("m.b")    // disjoint sibling

	// Write the nested path: fires the ancestor (m.a) and nested (m.a.x), NOT m.b.
	// Both deltas must be ROOT-ROOTED (the watch contract), so both carry m.a.x.v=1.
	if err := scoped.Patch(ctx, "m.a.x", vObj(1)); err != nil {
		t.Fatalf("scoped patch m.a.x: %v", err)
	}
	assertRootedDelta(t, "m.a", wA, "$.m.a.x.v", 1)
	assertRootedDelta(t, "m.a.x", wAX, "$.m.a.x.v", 1)
	expectQuiet(t, "m.b", wB)

	// Write the disjoint sibling: fires only m.b, root-rooted at m.b.
	if err := scoped.Patch(ctx, "m.b", vObj(2)); err != nil {
		t.Fatalf("scoped patch m.b: %v", err)
	}
	assertRootedDelta(t, "m.b", wB, "$.m.b.v", 2)
	expectQuiet(t, "m.a", wA)
	expectQuiet(t, "m.a.x", wAX)
}

// assertRootedDelta reads one event and checks its Patch is a valid ROOT-ROOTED
// delta: applying it (Patch(prior, delta)) must yield the value at kp. Since these
// are fresh null->value writes, applying to a null base suffices. This proves the
// re-rooting AND that the delta is a real patch (with ops), not just non-empty.
func assertRootedDelta(t *testing.T, name string, w *Watch, kp string, want int64) {
	t.Helper()
	ev := expectEvent(t, w)
	if ev.Patch == nil {
		t.Errorf("watch %s: expected a delta Patch, got %+v", name, ev)
		return
	}
	applied, err := tony.Patch(ir.Null(), ev.Patch)
	if err != nil {
		t.Errorf("watch %s: applying delta failed: %v", name, err)
		return
	}
	v, err := applied.GetPath(kp)
	if err != nil || v == nil || v.Int64 == nil || *v.Int64 != want {
		t.Errorf("watch %s: applied delta at %s = %v (err %v), want %d (root-rooted)", name, kp, v, err, want)
	}
}

// TestVerse_ScopedIncrementalDeltaPreservesSiblings proves the re-rooted delta is a
// genuine incremental patch, not a full-subtree replace: watching ancestor "m",
// after m.a is written, adding m.b must yield a delta that (applied to the prior
// state) leaves m.a intact. This is the property a naive "diff the whole subtree
// and replace" would break.
func TestVerse_ScopedIncrementalDeltaPreservesSiblings(t *testing.T) {
	logd := startLogd(t)
	ctx := context.Background()

	w := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "w", Scope: "s1"})
	t.Cleanup(func() { w.Close() })
	watch, err := w.Watch(ctx, "m", nil)
	if err != nil {
		t.Fatalf("watch m: %v", err)
	}
	expectEvent(t, watch) // drain init (null)

	writer := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "wr", Scope: "s1"})
	t.Cleanup(func() { writer.Close() })

	if err := writer.Patch(ctx, "m.a", vObj(1)); err != nil {
		t.Fatalf("patch m.a: %v", err)
	}
	state, err := tony.Patch(ir.Null(), expectEvent(t, watch).Patch)
	if err != nil {
		t.Fatalf("apply delta 1: %v", err)
	}

	if err := writer.Patch(ctx, "m.b", vObj(2)); err != nil {
		t.Fatalf("patch m.b: %v", err)
	}
	state, err = tony.Patch(state, expectEvent(t, watch).Patch) // apply incrementally
	if err != nil {
		t.Fatalf("apply delta 2: %v", err)
	}

	if v, _ := state.GetPath("$.m.a.v"); v == nil || v.Int64 == nil || *v.Int64 != 1 {
		t.Errorf("m.a clobbered by the m.b delta: got %v, want 1", v)
	}
	if v, _ := state.GetPath("$.m.b.v"); v == nil || v.Int64 == nil || *v.Int64 != 2 {
		t.Errorf("m.b not applied: got %v, want 2", v)
	}
}

// TestVerse_ScopedWatchCrosstalk_DirectLogd isolates the layer: two scoped watches
// on sibling paths held directly against logd (no docd, no controller). A scoped
// write to one path must fire only that watch.
func TestVerse_ScopedWatchCrosstalk_DirectLogd(t *testing.T) {
	logd := startLogd(t)
	ctx := context.Background()

	watcher := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "w", Scope: "s1"})
	t.Cleanup(func() { watcher.Close() })
	wa, err := watcher.Watch(ctx, "a", nil)
	if err != nil {
		t.Fatalf("watch a: %v", err)
	}
	wb, err := watcher.Watch(ctx, "b", nil)
	if err != nil {
		t.Fatalf("watch b: %v", err)
	}
	expectEvent(t, wa) // drain init
	expectEvent(t, wb) // drain init

	writer := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "wr", Scope: "s1"})
	t.Cleanup(func() { writer.Close() })
	if err := writer.Patch(ctx, "a", vObj(1)); err != nil {
		t.Fatalf("scoped patch a: %v", err)
	}

	if ev := expectEvent(t, wa); ev.Patch == nil {
		t.Errorf("watch a: expected delta, got %+v", ev)
	}
	expectQuiet(t, "b", wb) // b must NOT fire on a write to a
}

// TestVerse_WatchCrosstalk_DirectLogd_CommonParent is the decisive isolation:
// sibling watches under a COMMON PARENT (p.a, p.b) directly against logd. If a
// write to p.a fires the p.b watch here, the cross-talk is in logd's own
// event-matching (a write bubbling a notification for the shared ancestor p),
// independent of docd/controller. Run both scoped and baseline.
func TestVerse_WatchCrosstalk_DirectLogd_CommonParent(t *testing.T) {
	for _, scope := range []string{"", "s1"} {
		scope := scope
		name := "baseline"
		if scope != "" {
			name = "scoped"
		}
		t.Run(name, func(t *testing.T) {
			// Both baseline and scoped are now covered: the baseline watcher is woken
			// by the coarse wake but its forward is gated on whether its own subtree
			// changed (materialized), so a sibling write to p.a no longer reaches the
			// p.b watch; the scoped watcher trims+diffs its subtree.
			logd := startLogd(t)
			ctx := context.Background()
			cfg := func(id string) *LogdSessionConfig {
				c := &LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: id}
				if scope != "" {
					c.Scope = scope
				}
				return c
			}

			watcher := NewLogdSession(cfg("w"))
			t.Cleanup(func() { watcher.Close() })
			wa, err := watcher.Watch(ctx, "p.a", nil)
			if err != nil {
				t.Fatalf("watch p.a: %v", err)
			}
			wb, err := watcher.Watch(ctx, "p.b", nil)
			if err != nil {
				t.Fatalf("watch p.b: %v", err)
			}
			expectEvent(t, wa)
			expectEvent(t, wb)

			writer := NewLogdSession(cfg("wr"))
			t.Cleanup(func() { writer.Close() })
			if err := writer.Patch(ctx, "p.a", vObj(1)); err != nil {
				t.Fatalf("patch p.a: %v", err)
			}

			// p.a's own watch fires, and its forwarded delta is correct (applies to
			// the written value). Baseline forwards the raw committed delta.
			assertRootedDelta(t, "p.a", wa, "$.p.a.v", 1)
			expectQuiet(t, "p.b", wb) // p.b must NOT fire on a write to p.a
		})
	}
}

// TestVerse_BaselineWatchCrosstalk_DirectLogd is the baseline (unscoped) control:
// same as above but no scope. Tells us whether the cross-talk is scope-specific.
func TestVerse_BaselineWatchCrosstalk_DirectLogd(t *testing.T) {
	logd := startLogd(t)
	ctx := context.Background()

	watcher := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "w"})
	t.Cleanup(func() { watcher.Close() })
	wa, err := watcher.Watch(ctx, "a", nil)
	if err != nil {
		t.Fatalf("watch a: %v", err)
	}
	wb, err := watcher.Watch(ctx, "b", nil)
	if err != nil {
		t.Fatalf("watch b: %v", err)
	}
	expectEvent(t, wa)
	expectEvent(t, wb)

	writer := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "wr"})
	t.Cleanup(func() { writer.Close() })
	if err := writer.Patch(ctx, "a", vObj(1)); err != nil {
		t.Fatalf("patch a: %v", err)
	}

	if ev := expectEvent(t, wa); ev.Patch == nil {
		t.Errorf("watch a: expected delta, got %+v", ev)
	}
	expectQuiet(t, "b", wb)
}

// TestMultiWatch_SamePathDirectLogd: one session holds two watches on the SAME
// path; each is keyed by its own request id, so both receive every delta, and
// closing one (unwatch by id) leaves the other streaming.
func TestMultiWatch_SamePathDirectLogd(t *testing.T) {
	logd := startLogd(t)
	ctx := context.Background()

	sess := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "w"})
	t.Cleanup(func() { sess.Close() })
	w1, err := sess.Watch(ctx, "a", nil)
	if err != nil {
		t.Fatalf("watch 1: %v", err)
	}
	w2, err := sess.Watch(ctx, "a", nil) // same path — previously rejected, now allowed
	if err != nil {
		t.Fatalf("watch 2 on same path: %v", err)
	}
	expectEvent(t, w1) // drain init
	expectEvent(t, w2)

	writer := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "wr"})
	t.Cleanup(func() { writer.Close() })
	if err := writer.Patch(ctx, "a", vObj(1)); err != nil {
		t.Fatalf("patch a: %v", err)
	}
	assertRootedDelta(t, "w1", w1, "$.a.v", 1)
	assertRootedDelta(t, "w2", w2, "$.a.v", 1)

	// Unwatch one; the other keeps receiving.
	w1.Close()
	if err := writer.Patch(ctx, "a", vObj(2)); err != nil {
		t.Fatalf("patch a again: %v", err)
	}
	assertRootedDelta(t, "w2", w2, "$.a.v", 2)
}

// TestMultiWatch_SamePathViaDocd: two watches on the same path under a mount,
// through docd — exercises the docd id-restore on forwarded events and dropWatch
// by id. Both stream; closing one leaves the other.
func TestMultiWatch_SamePathViaDocd(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "m", newWatchableLogdController(t, logd.TCPAddr(), "cM"))

	client := docdClient(t, docd, "client")
	ctx := context.Background()

	w1, err := client.Watch(ctx, "m.a", nil)
	if err != nil {
		t.Fatalf("watch 1: %v", err)
	}
	w2, err := client.Watch(ctx, "m.a", nil)
	if err != nil {
		t.Fatalf("watch 2 on same path: %v", err)
	}
	expectEvent(t, w1) // drain init
	expectEvent(t, w2)

	if err := client.Patch(ctx, "m.a", vObj(1)); err != nil {
		t.Fatalf("patch m.a: %v", err)
	}
	assertRootedDelta(t, "w1", w1, "$.m.a.v", 1)
	assertRootedDelta(t, "w2", w2, "$.m.a.v", 1)

	w1.Close()
	if err := client.Patch(ctx, "m.a", vObj(2)); err != nil {
		t.Fatalf("patch m.a again: %v", err)
	}
	assertRootedDelta(t, "w2", w2, "$.m.a.v", 2)
}

// TestWatch_TwoSessionsSamePathNoRace is a -race regression: the hub broadcasts one
// shared notification.Patch to every watcher on a path, and encoding it mutates the
// node's parent linkage (ir.FromMap), so two separate sessions' writers serializing
// the same node raced. Each watcher must get its own copy. Run under `go test -race`.
func TestWatch_TwoSessionsSamePathNoRace(t *testing.T) {
	logd := startLogd(t)
	ctx := context.Background()
	s1 := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "s1"})
	t.Cleanup(func() { s1.Close() })
	s2 := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "s2"})
	t.Cleanup(func() { s2.Close() })
	w1, err := s1.Watch(ctx, "a", nil)
	if err != nil {
		t.Fatalf("watch s1: %v", err)
	}
	w2, err := s2.Watch(ctx, "a", nil)
	if err != nil {
		t.Fatalf("watch s2: %v", err)
	}
	expectEvent(t, w1) // drain init
	expectEvent(t, w2)

	writer := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "wr"})
	t.Cleanup(func() { writer.Close() })
	if err := writer.Patch(ctx, "a", vObj(1)); err != nil {
		t.Fatalf("patch: %v", err)
	}
	// Both sessions deliver (and encode) the broadcast concurrently.
	expectEvent(t, w1)
	expectEvent(t, w2)
}

// expectQuiet asserts no event arrives on w within a short window.
func expectQuiet(t *testing.T, p string, w *Watch) {
	t.Helper()
	select {
	case ev, ok := <-w.Events():
		if ok {
			t.Errorf("watch %s: unexpected event %+v", p, ev)
		} else {
			t.Errorf("watch %s: closed unexpectedly: %v", p, w.Err())
		}
	case <-time.After(300 * time.Millisecond):
	}
}
