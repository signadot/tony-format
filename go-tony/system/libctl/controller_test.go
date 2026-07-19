package libctl

import (
	"context"
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
			Path:       "/x",
			Handler:    ctrl,
		})
	}()
	waitMount(t, docd, "/x")

	client := docdClient(t, docd, "client")

	// Issue a match that will block inside the controller.
	matchErr := make(chan error, 1)
	go func() {
		_, err := client.Match(context.Background(), "x/1")
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
	waitTombstone(t, docd, "/x")
	if _, err := client.Match(context.Background(), "x/1"); err == nil {
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
			Path:       "/y",
			Handler:    newMemController(),
		})
	}()
	waitMount(t, docd, "/y")
	cancel1()
	<-errc1
	waitTombstone(t, docd, "/y")

	// A new controller remounts /y, clearing the tombstone.
	ctrl2 := newMemController()
	runController(t, docd, "/y", ctrl2)

	client := docdClient(t, docd, "client")
	if err := client.Patch(context.Background(), "y/1",
		ir.FromMap(map[string]*ir.Node{"v": ir.FromInt(1)})); err != nil {
		t.Fatalf("patch after remount failed: %v", err)
	}
	ctrl2.mu.Lock()
	_, ok := ctrl2.data["y/1"]
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

// TestDocd_MetaMounts proves docd serves its mount registry at .meta/mounts over
// the normal client protocol, including tombstoned (crashed) mounts.
func TestDocd_MetaMounts(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())

	// A live mount.
	runController(t, docd, "/users", newMemController())

	// A mount that crashes, leaving a tombstone.
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		errc <- RunController(ctx, &ControllerConfig{
			DocdAddr: docd.TCPAddr(), Controller: "gone-ctrl", Path: "/gone", Handler: newMemController(),
		})
	}()
	waitMount(t, docd, "/gone")
	cancel()
	<-errc
	waitTombstone(t, docd, "/gone")

	client := docdClient(t, docd, "admin")
	body, err := client.Match(context.Background(), ".meta/mounts")
	if err != nil {
		t.Fatalf("meta mounts match failed: %v", err)
	}

	rendered, err := gomap.ToString(body, gomap.EncodeWire(true))
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	for _, want := range []string{"/users", "live", "/gone", "tombstoned"} {
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
			DocdAddr: docd.TCPAddr(), Controller: "sc", Path: "/users",
			Schema: schema, Handler: newMemController(),
		})
	}()
	waitMount(t, docd, "/users")

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
			for _, want := range []string{"/users", "myschema"} {
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
