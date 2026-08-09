package libctl

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// matchFailingController serves everything a memController does except the read:
// its Match always fails, which is what makes a composed read fail without
// needing a crashed controller (that is controller_unavailable, a different
// answer).
type matchFailingController struct {
	*memController
}

func (c *matchFailingController) Match(ctx context.Context, path string, pattern *ir.Node, opts MatchParams) (*ir.Node, error) {
	return nil, errors.New("backing store exploded")
}

func newMatchFailingController() *matchFailingController {
	mc := newMemController()
	mc.watchable = true // so the composed watch's sub-watch still establishes
	return &matchFailingController{memController: mc}
}

// TestDocd_ComposedMatchFailureIsMatchFailed: a composed read that cannot read
// reports match_failed. It used to report session_closed, which named a condition
// that had not happened — the client's session was fine — so a client acting on
// the code would tear down and redial a healthy connection instead of dealing
// with the read that actually broke.
func TestDocd_ComposedMatchFailureIsMatchFailed(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "a.b", newMatchFailingController())

	client := docdClient(t, docd, "client")
	// "a" is an ancestor of the mount, so this match composes base + a.b.
	_, err := client.Match(context.Background(), "a")
	if err == nil {
		t.Fatal("expected the composed match to fail")
	}
	if got := api.ErrorCode(err); got != api.ErrCodeMatchFailed {
		t.Errorf("ErrorCode = %q, want %q (err: %v)", got, api.ErrCodeMatchFailed, err)
	}
	if got := api.ErrorCode(err); got == api.ErrCodeSessionClosed {
		t.Error("a failed read must not be reported as a closed session")
	}
}

// TestDocd_ComposedWatchInitFailureEndsWatch: the initial snapshot of a composed
// watch IS a match, and when it fails the watch has no baseline. This used to be
// swallowed — `if err == nil` — leaving the client a confirmed watch that never
// received state and then streamed deltas against a baseline it never got. The
// watch now ends with match_failed, which the client can re-establish from.
func TestDocd_ComposedWatchInitFailureEndsWatch(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "a.b", newMatchFailingController())

	client := docdClient(t, docd, "client")
	w, err := client.Watch(context.Background(), "a", nil)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	// The confirmation arrives before the snapshot is attempted, so the watch is
	// established and then ends — no initial state event in between.
	drainUntilClosed(t, w)
	var ended *WatchEndedError
	if !errors.As(w.Err(), &ended) || ended.Reason != api.ErrCodeMatchFailed {
		t.Fatalf("expected the watch to end with %q, got %v", api.ErrCodeMatchFailed, w.Err())
	}
}

// TestDocd_ComposedWatchInitSucceedsStillStreams guards the other side of the
// change: making the failure terminal must not make an ordinary composed watch
// terminal. A healthy mount still gets its initial snapshot and stays open.
func TestDocd_ComposedWatchInitSucceedsStillStreams(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	mc := newMemController()
	mc.watchable = true
	runController(t, docd, "a.b", mc)

	client := docdClient(t, docd, "client")
	w, err := client.Watch(context.Background(), "a", nil)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if ev := expectEvent(t, w); ev.State == nil {
		t.Fatal("expected an initial state event on a healthy composed watch")
	}
	select {
	case _, ok := <-w.Events():
		if !ok {
			t.Fatalf("healthy composed watch ended: %v", w.Err())
		}
	case <-time.After(200 * time.Millisecond):
		// Still open with nothing to say, which is what we want.
	}
}
