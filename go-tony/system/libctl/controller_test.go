package libctl

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	docdserver "github.com/signadot/tony-format/go-tony/system/docd/server"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	logdserver "github.com/signadot/tony-format/go-tony/system/logd/server"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// memController is a reference in-memory controller for tests. It stores patched
// data by path and answers matches from that store. When watchable is false it
// declines watches with ErrUnsupported (the read-only / connect-controller
// behavior); when true, each watch emits an initial state event and then any
// event passed to broadcast — which fans out to every active watcher, so
// concurrent watchers on the same path each receive it.
type memController struct {
	mu        sync.Mutex
	data      map[string]*ir.Node
	watchable bool
	subs      map[chan *api.WatchEvent]struct{}
}

func newMemController() *memController {
	return &memController{
		data: make(map[string]*ir.Node),
		subs: make(map[chan *api.WatchEvent]struct{}),
	}
}

func (c *memController) Match(ctx context.Context, path string, pattern *ir.Node) (*ir.Node, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v := c.data[path]; v != nil {
		return v, nil
	}
	return ir.Null(), nil
}

func (c *memController) Patch(ctx context.Context, path string, data *ir.Node, opts PatchParams) (*ir.Node, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[path] = data
	return data, nil
}

func (c *memController) Watch(ctx context.Context, path string, opts WatchParams, emit func(*api.WatchEvent) error) error {
	if !c.watchable {
		return ErrUnsupported
	}

	ch := make(chan *api.WatchEvent, 16)
	c.mu.Lock()
	st := c.data[path]
	c.subs[ch] = struct{}{} // registered before the confirming emit below
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.subs, ch)
		c.mu.Unlock()
	}()

	if !opts.NoInit {
		if err := emit(&api.WatchEvent{Commit: 1, Path: path, State: st}); err != nil {
			return err
		}
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev := <-ch:
			if err := emit(ev); err != nil {
				return err
			}
		}
	}
}

// broadcast fans an event out to every active watcher.
func (c *memController) broadcast(ev *api.WatchEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for ch := range c.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// subCount reports how many watches are currently subscribed.
func (c *memController) subCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.subs)
}

// logdController is a reference controller backed by logd: it reads via a logd
// session and, on patch, writes through to logd — joining a multi-participant
// transaction when the client supplies a TxID (the write is the join).
type logdController struct {
	logd *LogdSession
}

func newLogdController(t *testing.T, logdAddr, id string) *logdController {
	t.Helper()
	s := NewLogdSession(&LogdSessionConfig{Addr: logdAddr, ClientID: id})
	t.Cleanup(func() { s.Close() })
	return &logdController{logd: s}
}

func (c *logdController) Match(ctx context.Context, path string, pattern *ir.Node) (*ir.Node, error) {
	return c.logd.Match(ctx, path)
}

func (c *logdController) Patch(ctx context.Context, path string, data *ir.Node, opts PatchParams) (*ir.Node, error) {
	// Faithfully forward the routed participant (tx id, precondition, timeout).
	if err := c.logd.PatchWith(ctx, path, data, PatchOpts{
		TxID:    opts.TxID,
		Match:   opts.Match,
		Timeout: opts.Timeout,
	}); err != nil {
		return nil, err
	}
	return data, nil
}

func (c *logdController) Watch(ctx context.Context, path string, opts WatchParams, emit func(*api.WatchEvent) error) error {
	return ErrUnsupported
}

// TestDocd_MultiMountTransaction proves a client can commit a write spanning two
// mounts atomically through docd: NewTx is served from docd's pool, and the two
// PatchTx operations route to two logd-backed controllers that each join the
// transaction by writing to logd; logd commits both together.
func TestDocd_MultiMountTransaction(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())

	runController(t, docd, "a", newLogdController(t, logd.TCPAddr(), "ctrlA"))
	runController(t, docd, "b", newLogdController(t, logd.TCPAddr(), "ctrlB"))

	client := docdClient(t, docd, "client")
	ctx := context.Background()

	// NewTx(2) is served from docd's pool (no logd round trip).
	txID, err := client.NewTx(ctx, 2)
	if err != nil {
		t.Fatalf("NewTx via docd failed: %v", err)
	}

	// Two concurrent participant writes, routed to the two controllers; each
	// blocks until the transaction commits atomically.
	errc := make(chan error, 2)
	go func() {
		errc <- client.PatchTx(ctx, "a.1", ir.FromMap(map[string]*ir.Node{"v": ir.FromInt(1)}), txID)
	}()
	go func() {
		errc <- client.PatchTx(ctx, "b.1", ir.FromMap(map[string]*ir.Node{"v": ir.FromInt(2)}), txID)
	}()
	for i := 0; i < 2; i++ {
		if err := <-errc; err != nil {
			t.Fatalf("PatchTx via docd failed: %v", err)
		}
	}

	// Both writes committed and are visible in logd.
	direct := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "verify"})
	defer direct.Close()
	for path, want := range map[string]int64{"a.1": 1, "b.1": 2} {
		res, err := direct.Match(ctx, path)
		if err != nil {
			t.Fatalf("verify Match %s failed: %v", path, err)
		}
		v, err := res.GetPath("$.v")
		if err != nil || v == nil || v.Int64 == nil || *v.Int64 != want {
			t.Errorf("%s: expected v=%d, got %v (err %v)", path, want, v, err)
		}
	}
}

// TestDocd_ControllerCAS proves a compare-and-swap precondition routes through
// docd to a controller and is honored: the CAS succeeds while the precondition
// holds and fails (ErrMatchFailed) once it does not, with no write applied.
func TestDocd_ControllerCAS(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "doc", newLogdController(t, logd.TCPAddr(), "ctrlDoc"))

	client := docdClient(t, docd, "client")
	ctx := context.Background()
	vNode := func(n int64) *ir.Node { return ir.FromMap(map[string]*ir.Node{"v": ir.FromInt(n)}) }

	if err := client.Patch(ctx, "doc.1", vNode(1)); err != nil {
		t.Fatalf("seed via controller: %v", err)
	}

	match1 := &api.PathData{Path: "doc.1", Data: vNode(1)}
	if err := client.PatchIf(ctx, "doc.1", vNode(2), match1); err != nil {
		t.Fatalf("routed CAS should succeed: %v", err)
	}
	if err := client.PatchIf(ctx, "doc.1", vNode(3), match1); !errors.Is(err, ErrMatchFailed) {
		t.Fatalf("expected ErrMatchFailed through docd, got %v", err)
	}

	res, err := client.Match(ctx, "doc.1")
	if err != nil {
		t.Fatalf("verify Match: %v", err)
	}
	v, err := res.GetPath("$.v")
	if err != nil || v == nil || v.Int64 == nil || *v.Int64 != 2 {
		t.Errorf("expected v=2 after failed CAS, got %v (err %v)", v, err)
	}
}

func vObj(n int64) *ir.Node { return ir.FromMap(map[string]*ir.Node{"v": ir.FromInt(n)}) }

func assertLogdV(t *testing.T, s *LogdSession, path string, want int64) {
	t.Helper()
	res, err := s.Match(context.Background(), path)
	if err != nil {
		t.Fatalf("match %s: %v", path, err)
	}
	v, err := res.GetPath("$.v")
	if err != nil || v == nil || v.Int64 == nil || *v.Int64 != want {
		t.Errorf("%s: expected v=%d, got %v (err %v)", path, want, v, err)
	}
}

// TestDocd_MultiMountPatchSplit proves a single client patch spanning two mounts
// is decomposed and committed atomically through docd.
func TestDocd_MultiMountPatchSplit(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "a", newLogdController(t, logd.TCPAddr(), "cA"))
	runController(t, docd, "b", newLogdController(t, logd.TCPAddr(), "cB"))

	client := docdClient(t, docd, "client")
	ctx := context.Background()

	// One patch at root writing NESTED data under both mounts; reading the deeper
	// kpath (a.x) navigates into it (this is what the slash bug broke).
	data := ir.FromMap(map[string]*ir.Node{
		"a": ir.FromMap(map[string]*ir.Node{"x": vObj(1)}),
		"b": ir.FromMap(map[string]*ir.Node{"x": vObj(2)}),
	})
	if err := client.Patch(ctx, "", data); err != nil {
		t.Fatalf("multi-mount patch failed: %v", err)
	}

	direct := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "verify"})
	defer direct.Close()
	assertLogdV(t, direct, "a.x", 1)
	assertLogdV(t, direct, "b.x", 2)
}

// TestDocd_MultiMountPatchWithBase proves a patch spanning a mount and the base
// store commits atomically — the controller writes its subtree and docd writes
// the base remainder as the extra participant.
func TestDocd_MultiMountPatchWithBase(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "a", newLogdController(t, logd.TCPAddr(), "cA"))

	client := docdClient(t, docd, "client")
	ctx := context.Background()

	data := ir.FromMap(map[string]*ir.Node{"a": vObj(1), "cfg": vObj(9)})
	if err := client.Patch(ctx, "", data); err != nil {
		t.Fatalf("mount+base patch failed: %v", err)
	}

	direct := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "verify"})
	defer direct.Close()
	assertLogdV(t, direct, "a", 1)   // written by the controller
	assertLogdV(t, direct, "cfg", 9) // base remainder written by docd
}

// TestDocd_MultiMountUndecomposable proves docd rejects a patch it cannot split
// statically — a higher-order op above a mount boundary.
func TestDocd_MultiMountUndecomposable(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "a", newLogdController(t, logd.TCPAddr(), "cA"))
	runController(t, docd, "b", newLogdController(t, logd.TCPAddr(), "cB"))

	client := docdClient(t, docd, "client")
	ctx := context.Background()

	tagged := ir.FromMap(map[string]*ir.Node{"a": vObj(1)}).WithTag("!all")
	err := client.Patch(ctx, "", tagged)
	if err == nil {
		t.Fatal("expected error for undecomposable patch, got nil")
	}
	if !strings.Contains(err.Error(), "decompose") {
		t.Errorf("expected decomposition error, got %v", err)
	}
}

// TestDocd_MultiMountCASAbort proves a failed precondition aborts the whole
// multi-mount transaction — no participant's write is applied.
func TestDocd_MultiMountCASAbort(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "a", newLogdController(t, logd.TCPAddr(), "cA"))
	runController(t, docd, "b", newLogdController(t, logd.TCPAddr(), "cB"))

	client := docdClient(t, docd, "client")
	ctx := context.Background()

	// Seed a readable value at /a, then a multi-mount patch whose precondition on
	// "a" does not hold; the whole tx must abort.
	if err := client.Patch(ctx, "a", vObj(1)); err != nil {
		t.Fatalf("seed a: %v", err)
	}
	data := ir.FromMap(map[string]*ir.Node{"a": vObj(5), "b": vObj(6)})
	match := &api.PathData{Path: "a", Data: vObj(42)}
	if err := client.PatchIf(ctx, "", data, match); !errors.Is(err, ErrMatchFailed) {
		t.Fatalf("expected ErrMatchFailed, got %v", err)
	}

	// The aborted tx applied nothing: a is unchanged.
	direct := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "verify"})
	defer direct.Close()
	assertLogdV(t, direct, "a", 1)
}

// TestDocd_ComposeAncestorRead proves a client read whose path is a strict
// ancestor of a mount is composed from the base store (logd) AND the mounted
// subtree (a controller whose content does NOT live in logd) — the mount content
// a naive single-routed read would miss.
func TestDocd_ComposeAncestorRead(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())

	// In-memory controller mounted at a.b; its content lives in the controller,
	// not logd. Seed before runController so the write happens-before the session
	// goroutine reads it.
	mem := newMemController()
	mem.data["a.b"] = vObj(7)
	runController(t, docd, "a.b", mem)

	client := docdClient(t, docd, "client")
	ctx := context.Background()

	// Base content under a.x goes straight to logd (no mount owns it).
	if err := client.Patch(ctx, "a.x", vObj(1)); err != nil {
		t.Fatalf("base patch: %v", err)
	}

	// A read at the ancestor "a" must surface both the base (a.x) and the mount
	// (a.b) — the latter is invisible to a plain logd read.
	res, err := client.Match(ctx, "a")
	if err != nil {
		t.Fatalf("composed match: %v", err)
	}
	if v, err := res.GetPath("$.x.v"); err != nil || v == nil || v.Int64 == nil || *v.Int64 != 1 {
		t.Errorf("composed a.x.v: got %v (err %v), want 1", v, err)
	}
	if v, err := res.GetPath("$.b.v"); err != nil || v == nil || v.Int64 == nil || *v.Int64 != 7 {
		t.Errorf("composed a.b.v: got %v (err %v), want 7", v, err)
	}

	// Sanity: a plain logd read of "a" is blind to the mount — this is exactly the
	// naive gap composition closes.
	direct := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "verify"})
	defer direct.Close()
	naive, err := direct.Match(ctx, "a")
	if err != nil {
		t.Fatalf("naive logd match: %v", err)
	}
	if v, _ := naive.GetPath("$.b.v"); v != nil {
		t.Errorf("naive logd read unexpectedly saw the mount: %v", v)
	}
}

// TestDocd_ComposeNestedMountsOverlay proves that when a read spans nested mounts,
// the deeper mount overlays the shallower owner's stale slot: the base owner is
// itself a controller (mounted at a.b) and a nested controller (a.b.c) replaces
// the c subtree in the composed result.
func TestDocd_ComposeNestedMountsOverlay(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())

	owner := newMemController() // a.b: has a stale "c" plus its own "k"
	owner.data["a.b"] = ir.FromMap(map[string]*ir.Node{
		"k": vObj(1),
		"c": ir.FromMap(map[string]*ir.Node{"old": ir.FromInt(0)}),
	})
	nested := newMemController() // a.b.c: the authoritative c subtree
	nested.data["a.b.c"] = vObj(9)

	runController(t, docd, "a.b", owner)
	runController(t, docd, "a.b.c", nested)

	client := docdClient(t, docd, "client")
	ctx := context.Background()

	res, err := client.Match(ctx, "a.b")
	if err != nil {
		t.Fatalf("composed match: %v", err)
	}
	if v, err := res.GetPath("$.k.v"); err != nil || v == nil || v.Int64 == nil || *v.Int64 != 1 {
		t.Errorf("owner k.v: got %v (err %v), want 1", v, err)
	}
	if v, err := res.GetPath("$.c.v"); err != nil || v == nil || v.Int64 == nil || *v.Int64 != 9 {
		t.Errorf("nested c.v: got %v (err %v), want 9 (deeper mount must overlay)", v, err)
	}
	if v, _ := res.GetPath("$.c.old"); v != nil {
		t.Errorf("stale owner c.old should be replaced by the nested mount, got %v", v)
	}
}

// TestDocd_ComposeAncestorWatch proves a client watching an ancestor path gets a
// single composed initial snapshot (base + mount) and then live deltas from BOTH
// the base store (logd) and the mounted controller, each re-stamped to the watch
// path with its root-rooted patch intact.
func TestDocd_ComposeAncestorWatch(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())

	ctrl := newMemController()
	ctrl.watchable = true
	ctrl.data["a.b"] = vObj(7) // mount content, in the controller (not logd)
	runController(t, docd, "a.b", ctrl)

	client := docdClient(t, docd, "client")
	ctx := context.Background()

	if err := client.Patch(ctx, "a.x", vObj(1)); err != nil { // base content -> logd
		t.Fatalf("seed base: %v", err)
	}

	// Composed watch on the ancestor "a" (strict ancestor of mount a.b).
	w, err := client.Watch(ctx, "a", nil)
	if err != nil {
		t.Fatalf("watch a: %v", err)
	}
	defer w.Close()

	// 1. One composed initial State event: base a.x AND mount a.b.
	ev := expectEvent(t, w)
	if ev.State == nil {
		t.Fatalf("expected a composed initial State event, got %+v", ev)
	}
	if v, _ := ev.State.GetPath("$.x.v"); v == nil || v.Int64 == nil || *v.Int64 != 1 {
		t.Errorf("composed init a.x.v: got %v, want 1", v)
	}
	if v, _ := ev.State.GetPath("$.b.v"); v == nil || v.Int64 == nil || *v.Int64 != 7 {
		t.Errorf("composed init a.b.v: got %v, want 7", v)
	}

	// 2. A base delta (logd) streams through, re-stamped to Path "a", patch
	// root-rooted.
	if err := client.Patch(ctx, "a.x", vObj(2)); err != nil {
		t.Fatalf("base delta: %v", err)
	}
	ev = expectEvent(t, w)
	if ev.Path != "a" {
		t.Errorf("base delta path: got %q, want a", ev.Path)
	}
	if v, _ := ev.Patch.GetPath("$.a.x.v"); v == nil || v.Int64 == nil || *v.Int64 != 2 {
		t.Errorf("base delta a.x.v: got %v, want 2 (root-rooted)", v)
	}

	// 3. A mount delta (controller) streams through the same composed watch.
	waitSubs(t, ctrl, 1)
	ctrl.broadcast(&api.WatchEvent{Commit: 5, Path: "a.b", Patch: ir.FromMap(map[string]*ir.Node{
		"a": ir.FromMap(map[string]*ir.Node{"b": vObj(9)}),
	})})
	ev = expectEvent(t, w)
	if ev.Path != "a" {
		t.Errorf("mount delta path: got %q, want a", ev.Path)
	}
	if v, _ := ev.Patch.GetPath("$.a.b.v"); v == nil || v.Int64 == nil || *v.Int64 != 9 {
		t.Errorf("mount delta a.b.v: got %v, want 9 (root-rooted)", v)
	}
}

// TestDocd_MountBlocksOnOverlappingWatch proves a mount waits for an overlapping
// watch to drain: with force_after effectively infinite for the test window, a
// controller mounting a.b cannot register while a client watches the ancestor a,
// and proceeds the moment that watch is dropped.
func TestDocd_MountBlocksOnOverlappingWatch(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdForce(t, logd.TCPAddr(), 10*time.Second)

	client := docdClient(t, docd, "client")
	// A base watch on "a" (routed to logd) registers a reader overlapping a.b.
	w, err := client.Watch(context.Background(), "a", nil)
	if err != nil {
		t.Fatalf("watch a: %v", err)
	}

	// Launch a controller mounting a.b; its handshake must block on the watch.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		errc <- RunController(ctx, &ControllerConfig{
			DocdAddr: docd.TCPAddr(), Controller: "cB", Path: "a.b", Handler: newMemController(),
		})
	}()

	time.Sleep(300 * time.Millisecond)
	select {
	case err := <-errc:
		t.Fatalf("controller exited early: %v", err)
	default:
	}
	if docd.Mounts.Lookup("a.b").Live() {
		t.Fatal("mount registered despite an active overlapping watch")
	}

	// Dropping the watch drains the reader and the mount proceeds.
	w.Close()
	waitMount(t, docd, "a.b")
}

// TestDocd_MountForcesOverlappingWatch proves the mount is not blocked forever: a
// finite force_after force-ends the overlapping watch so the mount registers even
// while the client still holds it.
func TestDocd_MountForcesOverlappingWatch(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdForce(t, logd.TCPAddr(), 50*time.Millisecond)

	client := docdClient(t, docd, "client")
	w, err := client.Watch(context.Background(), "a", nil)
	if err != nil {
		t.Fatalf("watch a: %v", err)
	}
	defer w.Close()

	// runController asserts registration within 2s; the watch is never dropped, so
	// registering proves force_after force-ended it.
	runController(t, docd, "a.b", newMemController())
}

// TestDocd_WatchForcedMembershipChanged proves a force-ended watch reaches the
// client as a re-establishable WatchEndedError (not a silent stop): a mount whose
// force_after elapses ends the overlapping watch with membership_changed, and the
// client can re-watch (now composed over the new mount).
func TestDocd_WatchForcedMembershipChanged(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdForce(t, logd.TCPAddr(), 50*time.Millisecond)
	client := docdClient(t, docd, "client")
	ctx := context.Background()

	w, err := client.Watch(ctx, "a", nil) // base watch, overlapping the coming mount a.b
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	runController(t, docd, "a.b", newMemController()) // forces the watch after 50ms

	drainUntilClosed(t, w) // past the initial state event to the terminal close
	var ended *WatchEndedError
	if !errors.As(w.Err(), &ended) || ended.Reason != "membership_changed" {
		t.Fatalf("expected WatchEndedError membership_changed, got %v", w.Err())
	}

	// Re-watch succeeds (the path is now a composed ancestor of the mount).
	w2, err := client.Watch(ctx, "a", nil)
	if err != nil {
		t.Fatalf("re-watch after membership change: %v", err)
	}
	w2.Close()
}

// TestDocd_WatchEndsOnControllerCrash proves a watch on a mounted subtree ends the
// client with a re-establishable controller_unavailable error when the owning
// controller crashes, rather than silently stalling.
func TestDocd_WatchEndsOnControllerCrash(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())

	ctrl := newMemController()
	ctrl.watchable = true
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		errc <- RunController(ctx, &ControllerConfig{
			DocdAddr: docd.TCPAddr(), Controller: "crashing", Path: "rooms", Handler: ctrl,
		})
	}()
	waitMount(t, docd, "rooms")

	client := docdClient(t, docd, "client")
	w, err := client.Watch(context.Background(), "rooms.1", nil) // single-route to the controller
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	cancel() // crash the controller

	drainUntilClosed(t, w) // past the initial state event to the terminal close
	var ended *WatchEndedError
	if !errors.As(w.Err(), &ended) || ended.Reason != "controller_unavailable" {
		t.Fatalf("expected WatchEndedError controller_unavailable, got %v", w.Err())
	}
}

// drainUntilClosed reads and discards watch events until the channel closes,
// which is how a server-ended watch (WatchEndedError) surfaces after any initial
// or in-flight events.
func drainUntilClosed(t *testing.T, w *Watch) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-w.Events():
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("watch did not end")
		}
	}
}

// TestDocd_PerMountForceAfterOverride proves a controller's per-mount force_after
// overrides docd's server default: with a large default that would block the
// mount ~forever behind a held watch, a short per-mount force_after force-ends the
// watch so the mount registers.
func TestDocd_PerMountForceAfterOverride(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdForce(t, logd.TCPAddr(), 10*time.Second) // large server default

	client := docdClient(t, docd, "client")
	w, err := client.Watch(context.Background(), "a", nil)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer w.Close()

	short := 40 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		errc <- RunController(ctx, &ControllerConfig{
			DocdAddr: docd.TCPAddr(), Controller: "cB", Path: "a.b",
			Handler: newMemController(), ForceAfter: &short,
		})
	}()

	// Registers within waitMount's 2s only because the short override beat the 10s
	// default.
	waitMount(t, docd, "a.b")
}

// startDocdForce is startDocdRouting with a specific mount/unmount reader-drain
// timeout (mountCoord force_after).
func startDocdForce(t *testing.T, logdAddr string, forceAfter time.Duration) *docdserver.Server {
	t.Helper()
	srv := docdserver.New(&docdserver.Spec{LogdAddr: logdAddr, MountForceAfter: forceAfter})
	if err := srv.StartClientTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start docd client listener: %v", err)
	}
	t.Cleanup(func() { srv.StopClientTCP() })
	if err := srv.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start docd mount listener: %v", err)
	}
	t.Cleanup(func() { srv.StopTCP() })
	return srv
}

// startDocdRouting starts a docd with both listeners: the client face (logd
// session protocol) and the mount face (MOUNT), proxying/routing to logd.
func startDocdRouting(t *testing.T, logdAddr string) *docdserver.Server {
	t.Helper()
	srv := docdserver.New(&docdserver.Spec{LogdAddr: logdAddr})
	if err := srv.StartClientTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start docd client listener: %v", err)
	}
	t.Cleanup(func() { srv.StopClientTCP() })
	if err := srv.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start docd mount listener: %v", err)
	}
	t.Cleanup(func() { srv.StopTCP() })
	return srv
}

// runController launches a controller mounting path and waits for the mount to
// register with docd.
func runController(t *testing.T, docd *docdserver.Server, path string, h Handler) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errc := make(chan error, 1)
	go func() {
		errc <- RunController(ctx, &ControllerConfig{
			DocdAddr:   docd.TCPAddr(), // mount (controller-facing) listener
			Controller: "ctrl" + strings.ReplaceAll(path, "/", "-"),
			Path:       path,
			Handler:    h,
		})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-errc:
			t.Fatalf("controller exited early: %v", err)
		default:
		}
		if docd.Mounts.Lookup(path).Live() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("controller mount %q did not register", path)
}

// blockingMatchController blocks in Match until released or its context is
// cancelled, so a test can hold an operation in-flight and then crash the
// controller.
type blockingMatchController struct {
	*memController
	entered chan struct{}
	once    sync.Once
}

func (c *blockingMatchController) Match(ctx context.Context, path string, pattern *ir.Node) (*ir.Node, error) {
	c.once.Do(func() { close(c.entered) })
	<-ctx.Done()
	return ir.Null(), ctx.Err()
}

// waitMount waits for a controller mount to become live in docd.
func waitMount(t *testing.T, docd *docdserver.Server, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if docd.Mounts.Lookup(path).Live() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("mount %q did not register", path)
}

// waitTombstone waits for a mount to become tombstoned (present but not live).
func waitTombstone(t *testing.T, docd *docdserver.Server, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if e := docd.Mounts.Lookup(path); e != nil && !e.Live() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("mount %q did not tombstone", path)
}

// TestDocd_ControllerCrashFailsInflight proves an in-flight client operation
// fails deterministically (rather than hanging) when the owning controller
// crashes mid-operation.
func TestDocd_ControllerCrashFailsInflight(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())

	ctrl := &blockingMatchController{memController: newMemController(), entered: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		errc <- RunController(ctx, &ControllerConfig{
			DocdAddr:   docd.TCPAddr(),
			Controller: "crash-ctrl",
			Path:       "x",
			Handler:    ctrl,
		})
	}()
	waitMount(t, docd, "x")

	client := docdClient(t, docd, "client")

	// Issue a match that will block inside the controller.
	matchErr := make(chan error, 1)
	go func() {
		_, err := client.Match(context.Background(), "x.1")
		matchErr <- err
	}()

	// Wait until the operation is in-flight in the controller, then crash it.
	select {
	case <-ctrl.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("match never reached the controller")
	}
	cancel() // crash the controller

	// The in-flight op must return an error, not hang.
	select {
	case err := <-matchErr:
		if err == nil {
			t.Fatal("expected an error after controller crash, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight operation hung after controller crash")
	}
	<-errc

	// The mount is now tombstoned: a fresh op on the subtree fails with a clear
	// error rather than silently falling through to logd (which has no such key).
	waitTombstone(t, docd, "x")
	if _, err := client.Match(context.Background(), "x.1"); err == nil {
		t.Fatal("expected error on crashed-controller subtree, got nil")
	} else if !strings.Contains(err.Error(), "unavailable") {
		t.Errorf("expected controller_unavailable error, got %v", err)
	}
}

// TestDocd_RemountClearsTombstone proves a controller can remount a path whose
// previous controller crashed, restoring service to the subtree.
func TestDocd_RemountClearsTombstone(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())

	// First controller mounts /y then crashes.
	ctx1, cancel1 := context.WithCancel(context.Background())
	errc1 := make(chan error, 1)
	go func() {
		errc1 <- RunController(ctx1, &ControllerConfig{
			DocdAddr:   docd.TCPAddr(),
			Controller: "c1",
			Path:       "y",
			Handler:    newMemController(),
		})
	}()
	waitMount(t, docd, "y")
	cancel1()
	<-errc1
	waitTombstone(t, docd, "y")

	// A new controller remounts /y, clearing the tombstone.
	ctrl2 := newMemController()
	runController(t, docd, "y", ctrl2)

	client := docdClient(t, docd, "client")
	if err := client.Patch(context.Background(), "y.1",
		ir.FromMap(map[string]*ir.Node{"v": ir.FromInt(1)})); err != nil {
		t.Fatalf("patch after remount failed: %v", err)
	}
	ctrl2.mu.Lock()
	_, ok := ctrl2.data["y.1"]
	ctrl2.mu.Unlock()
	if !ok {
		t.Fatal("remounted controller did not receive the patch")
	}
}

// freeAddr reserves and releases a loopback TCP address so a server can be
// (re)started on it shortly after.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freeAddr: %v", err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

// startLogdAt starts a logd server on a specific address.
func startLogdAt(t *testing.T, addr string) *logdserver.Server {
	t.Helper()
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	srv := logdserver.New(&logdserver.Spec{Storage: store})
	if err := srv.StartTCP(addr); err != nil {
		t.Fatalf("failed to start logd at %s: %v", addr, err)
	}
	t.Cleanup(func() { srv.StopTCP() })
	return srv
}

// TestDocd_LogdDialBackoff proves docd retries its logd dial with backoff: a
// client connects while logd is down, and its operation completes once logd
// comes up (rather than failing immediately).
func TestDocd_LogdDialBackoff(t *testing.T) {
	addr := freeAddr(t) // logd not started yet

	docd := docdserver.New(&docdserver.Spec{LogdAddr: addr})
	if err := docd.StartClientTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start docd client listener: %v", err)
	}
	t.Cleanup(func() { docd.StopClientTCP() })

	client := docdClient(t, docd, "client")

	// The client connects to docd immediately; docd retries the logd dial while
	// logd is down.
	matchErr := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := client.Match(ctx, "") // root match returns cleanly on an empty store
		matchErr <- err
	}()

	// Bring logd up after a delay; the pending operation should then complete.
	time.Sleep(300 * time.Millisecond)
	startLogdAt(t, addr)

	select {
	case err := <-matchErr:
		if err != nil {
			t.Fatalf("expected match to succeed once logd started: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("match did not complete after logd started")
	}
}

func docdClient(t *testing.T, docd *docdserver.Server, id string) *LogdSession {
	t.Helper()
	s := NewLogdSession(&LogdSessionConfig{Addr: docd.ClientTCPAddr(), ClientID: id})
	t.Cleanup(func() { s.Close() })
	return s
}

// TestDocd_RouteMountedToController proves ops under a mounted subtree are routed
// to the owning controller (not logd): the patch lands in the controller's store
// and the match reads it back.
func TestDocd_RouteMountedToController(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	ctrl := newMemController()
	runController(t, docd, "users", ctrl)

	client := docdClient(t, docd, "client")
	ctx := context.Background()

	data := ir.FromMap(map[string]*ir.Node{"name": ir.FromString("alice")})
	if err := client.Patch(ctx, "users.1", data); err != nil {
		t.Fatalf("Patch failed: %v", err)
	}

	// The patch landed in the controller, not logd.
	ctrl.mu.Lock()
	stored := ctrl.data["users.1"]
	ctrl.mu.Unlock()
	if stored == nil {
		t.Fatal("controller did not receive the routed patch")
	}

	res, err := client.Match(ctx, "users.1")
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	name, err := res.GetPath("$.name")
	if err != nil || name == nil || name.String != "alice" {
		t.Errorf("expected name='alice' from controller, got %v (err %v)", name, err)
	}
}

// TestDocd_BasePathToLogd proves an op on a path not under any mount goes
// straight to logd: it is visible to a client talking to logd directly, and does
// not reach the controller.
func TestDocd_BasePathToLogd(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	ctrl := newMemController()
	runController(t, docd, "users", ctrl)

	client := docdClient(t, docd, "client")
	ctx := context.Background()

	data := ir.FromMap(map[string]*ir.Node{"theme": ir.FromString("dark")})
	if err := client.Patch(ctx, "config.1", data); err != nil {
		t.Fatalf("base Patch failed: %v", err)
	}

	// Not routed to the controller.
	ctrl.mu.Lock()
	_, inCtrl := ctrl.data["config.1"]
	ctrl.mu.Unlock()
	if inCtrl {
		t.Error("base-path patch wrongly reached the controller")
	}

	// Visible in logd directly.
	direct := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "direct"})
	defer direct.Close()
	res, err := direct.Match(ctx, "config.1")
	if err != nil {
		t.Fatalf("direct logd Match failed: %v", err)
	}
	theme, err := res.GetPath("$.theme")
	if err != nil || theme == nil || theme.String != "dark" {
		t.Errorf("expected theme='dark' in logd, got %v (err %v)", theme, err)
	}
}

// TestDocd_WatchUnsupported proves a controller can decline an operation and docd
// relays it: a watch on a watch-less controller returns the unsupported error.
func TestDocd_WatchUnsupported(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "users", newMemController()) // events nil => declines watch

	client := docdClient(t, docd, "client")
	ctx := context.Background()

	_, err := client.Watch(ctx, "users.1", nil)
	if err == nil {
		t.Fatal("expected watch to be declined")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected unsupported error, got %v", err)
	}
}

// TestDocd_WatchStreaming proves watch events stream from a controller through
// docd to the client: the initial state event, then a pushed update.
func TestDocd_WatchStreaming(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	ctrl := newMemController()
	ctrl.watchable = true
	runController(t, docd, "rooms", ctrl)

	client := docdClient(t, docd, "client")
	ctx := context.Background()

	w, err := client.Watch(ctx, "rooms.1", nil)
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	defer w.Close()

	// Initial state event.
	if ev := expectEvent(t, w); ev.Path != "rooms.1" {
		t.Errorf("expected initial event path rooms/1, got %q", ev.Path)
	}

	// A pushed update streams through the controller and docd to the client. Per
	// the canonical contract the delta Patch is root-rooted (absolute from the
	// document root), while Path names the watch.
	waitSubs(t, ctrl, 1)
	ctrl.broadcast(&api.WatchEvent{
		Commit: 2,
		Path:   "rooms.1",
		Patch: ir.FromMap(map[string]*ir.Node{
			"rooms": ir.FromMap(map[string]*ir.Node{
				"1": ir.FromMap(map[string]*ir.Node{"occupants": ir.FromInt(3)}),
			}),
		}),
	})
	if ev := expectEvent(t, w); ev.Commit != 2 {
		t.Errorf("expected update event commit 2, got %d", ev.Commit)
	}
}

// TestDocd_WatchMultiClientSamePath proves that two clients watching the SAME
// mounted path over the one controller connection are independent: both receive
// broadcast events, and one unwatching does not disturb the other.
func TestDocd_WatchMultiClientSamePath(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	ctrl := newMemController()
	ctrl.watchable = true
	runController(t, docd, "rooms", ctrl)

	ctx := context.Background()
	clientA := docdClient(t, docd, "A")
	clientB := docdClient(t, docd, "B")

	wA, err := clientA.Watch(ctx, "rooms.9", nil)
	if err != nil {
		t.Fatalf("A watch: %v", err)
	}
	defer wA.Close()
	expectEvent(t, wA) // initial state

	wB, err := clientB.Watch(ctx, "rooms.9", nil)
	if err != nil {
		t.Fatalf("B watch: %v", err)
	}
	defer wB.Close()
	expectEvent(t, wB) // initial state

	// Both watches are live on the one controller connection.
	waitSubs(t, ctrl, 2)

	// A broadcast reaches both clients.
	ctrl.broadcast(&api.WatchEvent{Commit: 5, Path: "rooms.9",
		Patch: ir.FromMap(map[string]*ir.Node{"n": ir.FromInt(1)})})
	if ev := expectEvent(t, wA); ev.Commit != 5 {
		t.Errorf("A: expected commit 5, got %d", ev.Commit)
	}
	if ev := expectEvent(t, wB); ev.Commit != 5 {
		t.Errorf("B: expected commit 5, got %d", ev.Commit)
	}

	// A unwatches; only A's controller-side watch is cancelled.
	if err := wA.Close(); err != nil {
		t.Fatalf("A close: %v", err)
	}
	waitSubs(t, ctrl, 1)

	// B still receives events.
	ctrl.broadcast(&api.WatchEvent{Commit: 6, Path: "rooms.9",
		Patch: ir.FromMap(map[string]*ir.Node{"n": ir.FromInt(2)})})
	if ev := expectEvent(t, wB); ev.Commit != 6 {
		t.Errorf("B: expected commit 6 after A left, got %d", ev.Commit)
	}
}

// TestDocd_MetaMounts proves docd serves its mount registry at .meta/mounts over
// the normal client protocol, including tombstoned (crashed) mounts.
func TestDocd_MetaMounts(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())

	// A live mount.
	runController(t, docd, "users", newMemController())

	// A mount that crashes, leaving a tombstone.
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		errc <- RunController(ctx, &ControllerConfig{
			DocdAddr: docd.TCPAddr(), Controller: "gone-ctrl", Path: "gone", Handler: newMemController(),
		})
	}()
	waitMount(t, docd, "gone")
	cancel()
	<-errc
	waitTombstone(t, docd, "gone")

	client := docdClient(t, docd, "admin")
	body, err := client.Match(context.Background(), ".meta/mounts")
	if err != nil {
		t.Fatalf("meta mounts match failed: %v", err)
	}

	rendered, err := gomap.ToString(body, gomap.EncodeWire(true))
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	for _, want := range []string{"users", "live", "gone", "tombstoned"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("meta mounts missing %q; got: %s", want, rendered)
		}
	}
}

// TestDocd_MetaSchemaAndIndex proves docd serves per-mount schema contributions
// at .meta/schema and a resource index at .meta, and that concurrent schema
// reads are safe (the stored schema node is cloned per response).
func TestDocd_MetaSchemaAndIndex(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())

	schema := ir.FromMap(map[string]*ir.Node{"marker": ir.FromString("myschema")})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		errc <- RunController(ctx, &ControllerConfig{
			DocdAddr: docd.TCPAddr(), Controller: "sc", Path: "users",
			Schema: schema, Handler: newMemController(),
		})
	}()
	waitMount(t, docd, "users")

	client := docdClient(t, docd, "admin")

	// .meta lists available resources.
	idx, err := client.Match(context.Background(), ".meta")
	if err != nil {
		t.Fatalf("meta index match failed: %v", err)
	}
	idxStr, _ := gomap.ToString(idx, gomap.EncodeWire(true))
	for _, want := range []string{"mounts", "schema"} {
		if !strings.Contains(idxStr, want) {
			t.Errorf(".meta index missing %q; got: %s", want, idxStr)
		}
	}

	// Concurrent .meta/schema reads (two clients) must both succeed and see the
	// contribution — exercises the clone-per-response path under -race.
	client2 := docdClient(t, docd, "admin2")
	var wg sync.WaitGroup
	for _, c := range []*LogdSession{client, client2} {
		wg.Add(1)
		go func(sess *LogdSession) {
			defer wg.Done()
			body, err := sess.Match(context.Background(), ".meta/schema")
			if err != nil {
				t.Errorf("meta schema match failed: %v", err)
				return
			}
			s, _ := gomap.ToString(body, gomap.EncodeWire(true))
			for _, want := range []string{"users", "myschema"} {
				if !strings.Contains(s, want) {
					t.Errorf(".meta/schema missing %q; got: %s", want, s)
				}
			}
		}(c)
	}
	wg.Wait()
}

// expectEvent reads the next event from a watch within a timeout.
func expectEvent(t *testing.T, w *Watch) *api.WatchEvent {
	t.Helper()
	select {
	case ev, ok := <-w.Events():
		if !ok {
			t.Fatalf("watch closed unexpectedly: %v", w.Err())
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for watch event")
		return nil
	}
}

// waitSubs waits until the controller reports want active watch subscriptions.
func waitSubs(t *testing.T, ctrl *memController, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ctrl.subCount() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("controller watch subscriptions = %d, want %d", ctrl.subCount(), want)
}
