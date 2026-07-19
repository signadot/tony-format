package libctl

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	docdserver "github.com/signadot/tony-format/go-tony/system/docd/server"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
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

func (c *memController) Patch(ctx context.Context, path string, data *ir.Node) (*ir.Node, error) {
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
		if docd.Mounts.Lookup(path) != nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("controller mount %q did not register", path)
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
	runController(t, docd, "/users", ctrl)

	client := docdClient(t, docd, "client")
	ctx := context.Background()

	data := ir.FromMap(map[string]*ir.Node{"name": ir.FromString("alice")})
	if err := client.Patch(ctx, "users/1", data); err != nil {
		t.Fatalf("Patch failed: %v", err)
	}

	// The patch landed in the controller, not logd.
	ctrl.mu.Lock()
	stored := ctrl.data["users/1"]
	ctrl.mu.Unlock()
	if stored == nil {
		t.Fatal("controller did not receive the routed patch")
	}

	res, err := client.Match(ctx, "users/1")
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
	runController(t, docd, "/users", ctrl)

	client := docdClient(t, docd, "client")
	ctx := context.Background()

	data := ir.FromMap(map[string]*ir.Node{"theme": ir.FromString("dark")})
	if err := client.Patch(ctx, "config/1", data); err != nil {
		t.Fatalf("base Patch failed: %v", err)
	}

	// Not routed to the controller.
	ctrl.mu.Lock()
	_, inCtrl := ctrl.data["config/1"]
	ctrl.mu.Unlock()
	if inCtrl {
		t.Error("base-path patch wrongly reached the controller")
	}

	// Visible in logd directly.
	direct := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "direct"})
	defer direct.Close()
	res, err := direct.Match(ctx, "config/1")
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
	runController(t, docd, "/users", newMemController()) // events nil => declines watch

	client := docdClient(t, docd, "client")
	ctx := context.Background()

	_, err := client.Watch(ctx, "users/1", nil)
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
	runController(t, docd, "/rooms", ctrl)

	client := docdClient(t, docd, "client")
	ctx := context.Background()

	w, err := client.Watch(ctx, "rooms/1", nil)
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	defer w.Close()

	// Initial state event.
	if ev := expectEvent(t, w); ev.Path != "rooms/1" {
		t.Errorf("expected initial event path rooms/1, got %q", ev.Path)
	}

	// A pushed update streams through the controller and docd to the client.
	waitSubs(t, ctrl, 1)
	ctrl.broadcast(&api.WatchEvent{
		Commit: 2,
		Path:   "rooms/1",
		Patch:  ir.FromMap(map[string]*ir.Node{"occupants": ir.FromInt(3)}),
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
	runController(t, docd, "/rooms", ctrl)

	ctx := context.Background()
	clientA := docdClient(t, docd, "A")
	clientB := docdClient(t, docd, "B")

	wA, err := clientA.Watch(ctx, "rooms/9", nil)
	if err != nil {
		t.Fatalf("A watch: %v", err)
	}
	defer wA.Close()
	expectEvent(t, wA) // initial state

	wB, err := clientB.Watch(ctx, "rooms/9", nil)
	if err != nil {
		t.Fatalf("B watch: %v", err)
	}
	defer wB.Close()
	expectEvent(t, wB) // initial state

	// Both watches are live on the one controller connection.
	waitSubs(t, ctrl, 2)

	// A broadcast reaches both clients.
	ctrl.broadcast(&api.WatchEvent{Commit: 5, Path: "rooms/9",
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
	ctrl.broadcast(&api.WatchEvent{Commit: 6, Path: "rooms/9",
		Patch: ir.FromMap(map[string]*ir.Node{"n": ir.FromInt(2)})})
	if ev := expectEvent(t, wB); ev.Commit != 6 {
		t.Errorf("B: expected commit 6 after A left, got %d", ev.Commit)
	}
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
