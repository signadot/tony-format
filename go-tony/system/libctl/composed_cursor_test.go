package libctl

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
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

	// A cursor whose deltas are kept and whose state is not: a watch starts from the
	// state at its cursor, and beyond compaction's cutoff logd answers that at the first
	// snapshot at or after it. The composed watch starts there, every sub-watch with it
	// (er3dnqpkh12krsb2qxn0). A scope's commit, which compaction keeps, stands between
	// the last baseline write and the snapshot, so the floor is that write and the
	// state there is gone.
	store := logd.Spec.Storage
	cursor, _ := store.GetCurrentCommit()
	sandbox := "sandbox"
	tx, err := store.NewTx(1, &sandbox)
	if err != nil {
		t.Fatalf("NewTx: %v", err)
	}
	p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: "elsewhere", Data: ir.FromInt(1)}})
	if err != nil {
		t.Fatalf("NewPatcher: %v", err)
	}
	if r := p.Commit(); !r.Committed {
		t.Fatalf("scoped commit: %v", r.Error)
	}
	snapshot := cursor + 1
	if err := store.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	cfg := storage.DefaultCompactionConfig()
	cfg.Cutoff = -time.Hour
	if err := store.Compact(cfg); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if floor := store.ReplayFloor(); floor != cursor {
		t.Fatalf("floor %d, want the cursor %d", floor, cursor)
	}
	if _, err := client.Patch(ctx, "verse.a.after", ir.FromInt(1)); err != nil {
		t.Fatalf("write after compaction: %s", err)
	}

	moved, err := client.Watch(ctx, "verse", &WatchOptions{FromCommit: &cursor})
	if err != nil {
		t.Fatalf("composed watch from %d: %s", cursor, err)
	}
	defer moved.Close()
	if from := moved.ReplayingFrom(); from == nil || *from != snapshot {
		t.Errorf("replaying from %v, want the snapshot's %d", from, snapshot)
	}
	state, deltas := int64(-1), 0
	deadline = time.After(5 * time.Second)
	for done := false; !done; {
		select {
		case ev, ok := <-moved.Events():
			switch {
			case !ok:
				t.Fatalf("composed watch from %d closed: %v", cursor, moved.Err())
			case ev.ReplayComplete:
				done = true
			case ev.State != nil:
				state = ev.Commit
			default:
				deltas++
				if ev.Commit <= snapshot {
					t.Errorf("delta at commit %d is at or below the state's %d", ev.Commit, snapshot)
				}
			}
		case <-deadline:
			t.Fatalf("no replay-complete from %d: state at %d, %d deltas", cursor, state, deltas)
		}
	}
	if state != snapshot || deltas != 1 {
		t.Errorf("state at %d and %d deltas, want the state at %d and the one write after it", state, deltas, snapshot)
	}

	// A client holding its own state is sent none to move it, and its watch is ended as
	// one from below the floor is -- established, then ended with replay_compacted --
	// whether docd read the state to find out or logd ended the sub-watch.
	for _, opts := range []*WatchOptions{
		{FromCommit: &cursor, NoInit: true},
		{FromCommit: &cursor, NoInit: true, WaitIfAbsent: true},
	} {
		w, err := client.Watch(ctx, "verse", opts)
		if err != nil {
			t.Errorf("noInit from %d (waitIfAbsent %v) was refused, not ended: %v", cursor, opts.WaitIfAbsent, err)
			continue
		}
		ended := time.After(5 * time.Second)
		for open := true; open; {
			select {
			case ev, ok := <-w.Events():
				if open = ok; ok {
					t.Errorf("noInit from %d (waitIfAbsent %v) delivered an event at commit %d", cursor, opts.WaitIfAbsent, ev.Commit)
				}
			case <-ended:
				t.Fatalf("noInit from %d (waitIfAbsent %v) did not end", cursor, opts.WaitIfAbsent)
			}
		}
		var end *WatchEndedError
		if !errors.As(w.Err(), &end) || end.Reason != api.ErrCodeReplayCompacted {
			t.Errorf("noInit from %d (waitIfAbsent %v) ended with %v, want %s", cursor, opts.WaitIfAbsent, w.Err(), api.ErrCodeReplayCompacted)
		}
		w.Close()
	}
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

	w, err := client.Watch(ctx, "verse", waitAbsent)
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
