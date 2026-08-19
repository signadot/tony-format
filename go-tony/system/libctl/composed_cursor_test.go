package libctl

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// watchingLogdController is a logd-backed controller which serves watches by delegating
// to logd -- including the cursor it was given. That is what a conforming mount does with
// a commit: the mount commits through the same logd under docd's transaction id, so the
// commit it is handed means the same thing to it as to every other mount, and it can
// simply pass it on.
type watchingLogdController struct {
	*logdController
}

func (c *watchingLogdController) Watch(ctx context.Context, path string, opts WatchParams, emit func(*api.WatchEvent) error) error {
	sess := c.session(opts.Scope)
	if err := sess.Connect(ctx); err != nil {
		return err
	}
	w, err := sess.Watch(ctx, path, &WatchOptions{FromCommit: opts.FromCommit, NoInit: opts.NoInit})
	if err != nil {
		return err
	}
	defer w.Close()
	for {
		select {
		case ev, ok := <-w.Events():
			if !ok {
				return w.Err()
			}
			if err := emit(ev); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// A composed watch -- one whose path spans several mounts -- resumes from a commit, and
// the deltas it replays arrive in commit order. Mounts share the commit sequence for
// their lifetime, so a cursor means the same thing to all of them; docd resolves it once
// and hands the same commit to every sub-watch (4ses3fqsh12ks8awgnn0).
func TestComposedWatchResumesFromACommit(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "verse.a", &watchingLogdController{newLogdController(t, logd.TCPAddr(), "ctrlA")})
	runController(t, docd, "verse.b", &watchingLogdController{newLogdController(t, logd.TCPAddr(), "ctrlB")})

	client := docdClient(t, docd, "client")
	ctx := context.Background()

	// Writes to both mounts, alternating, so a correct replay has to interleave them.
	for i := 0; i < 6; i++ {
		mount := "verse.a"
		if i%2 == 1 {
			mount = "verse.b"
		}
		if _, err := client.Patch(ctx, mount+".n"+strconv.Itoa(i),
			ir.FromMap(map[string]*ir.Node{"i": ir.FromInt(int64(i))})); err != nil {
			t.Fatalf("write %d: %s", i, err)
		}
	}

	back := int64(-4)
	w, err := client.Watch(ctx, "verse", &WatchOptions{FromCommit: &back})
	if err != nil {
		t.Fatalf("composed watch: %s", err)
	}
	defer w.Close()

	from, to := w.ReplayingFrom(), w.ReplayingTo()
	if from == nil {
		t.Fatal("a composed watch with a cursor reports no replay range: the cursor was dropped")
	}
	if to == nil || *to <= *from {
		t.Errorf("replaying from %d to %v", *from, to)
	}
	t.Logf("composed watch replaying from %d to %d", *from, *to)

	// The state, then deltas in commit order, then one replay-complete.
	var last int64
	seen := map[int64]int{}
	sawState, replays, completes := false, 0, 0
	deadline := time.After(5 * time.Second)
	for completes == 0 {
		select {
		case ev, ok := <-w.Events():
			if !ok {
				t.Fatalf("composed watch closed: %v", w.Err())
			}
			switch {
			case ev.ReplayComplete:
				completes++
			case ev.State != nil:
				sawState = true
				last = ev.Commit
			default:
				replays++
				seen[ev.Commit]++
				if ev.Commit < last {
					t.Errorf("delta at commit %d arrived after %d: the replay is out of order", ev.Commit, last)
				}
				if ev.Commit <= *from {
					t.Errorf("delta at commit %d is at or below the cursor %d", ev.Commit, *from)
				}
				last = ev.Commit
			}
		case <-deadline:
			t.Fatalf("no replay-complete: state=%v deltas=%d", sawState, replays)
		}
	}
	if !sawState {
		t.Error("the composed watch sent no initial state")
	}
	if replays == 0 {
		t.Error("the composed watch replayed no deltas though it reported a range")
	}
	// Once per commit. Every write here lands in ONE mount, so one commit is one
	// delta: the sub-watch on the composed path is trimmed to what that path owns, and
	// the mount's own stream carries the rest (hs9fge9rh12ksztzgnn0). A write spanning
	// two mounts is one commit with two disjoint halves, and would legitimately arrive
	// as two events at that commit.
	for commit, n := range seen {
		if commit <= *from || commit > *to {
			t.Errorf("a delta at commit %d is outside the range %d..%d", commit, *from, *to)
		}
		if n != 1 {
			t.Errorf("commit %d arrived %d times; each of these writes is one mount's", commit, n)
		}
	}
	if completes != 1 {
		t.Errorf("%d replay-complete events; a composed replay is one replay", completes)
	}
	t.Logf("replayed %d deltas in order, one replay-complete; per commit: %v", replays, seen)
}

// One write is one delta, live. The sub-watch on the composed path sees the whole subtree
// -- a logd-backed mount commits to the same logd, so its deltas come back there as well
// as on the mount's own stream -- and forwarding both delivered every commit twice. That
// is harmless for a field write and wrong for an operation, since !arraydiff applied twice
// is not !arraydiff applied once (hs9fge9rh12ksztzgnn0).
func TestComposedWatchDeliversEachCommitOnce(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "verse.a", &watchingLogdController{newLogdController(t, logd.TCPAddr(), "ctrlA")})

	client := docdClient(t, docd, "client")
	ctx := context.Background()
	if _, err := client.Patch(ctx, "verse.a.seed", ir.FromMap(map[string]*ir.Node{"n": ir.FromInt(0)})); err != nil {
		t.Fatalf("seed: %s", err)
	}

	w, err := client.Watch(ctx, "verse", nil)
	if err != nil {
		t.Fatalf("watch: %s", err)
	}
	defer w.Close()
	// initial state
	select {
	case ev, ok := <-w.Events():
		if !ok || ev.State == nil {
			t.Fatalf("no initial state: ok=%v ev=%+v", ok, ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no initial state")
	}

	if _, err := client.Patch(ctx, "verse.a.one", ir.FromMap(map[string]*ir.Node{"n": ir.FromInt(1)})); err != nil {
		t.Fatalf("write: %s", err)
	}

	seen := map[int64]int{}
	deadline := time.After(1500 * time.Millisecond)
	for {
		select {
		case ev, ok := <-w.Events():
			if !ok {
				t.Fatalf("watch closed: %v", w.Err())
			}
			if ev.Patch != nil {
				seen[ev.Commit]++
			}
		case <-deadline:
			t.Logf("live deltas per commit for ONE write: %v", seen)
			if len(seen) == 0 {
				t.Fatal("the write produced no delta at all")
			}
			for c, n := range seen {
				if n != 1 {
					t.Errorf("commit %d delivered %d times: the composed path's stream and the mount's both carry it", c, n)
				}
			}
			return
		}
	}
}
