package libctl

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// TestDocd_WatchEndReasonNamesMountVsUnmount is the point of the two codes: the
// same watch path, force-ended twice by the same controller, reports which
// direction the mount set moved each time.
//
// Both endings used to be "membership_changed", which told a client that
// something under it had changed but not what — and the two are different news. A
// mount means a subtree it was reading from base will now be answered by a
// controller, so the re-watch may legitimately see different content. An unmount
// means a source it was composing over is gone, so content may have disappeared.
// A client that only re-watches cannot tell the difference and does not need to;
// one that reconciles, reports, or decides whether the change it just saw was
// expected, does.
func TestDocd_WatchEndReasonNamesMountVsUnmount(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdForce(t, logd.TCPAddr(), 50*time.Millisecond)
	client := docdClient(t, docd, "client")
	ctx := context.Background()

	// A watch on the base path "a", overlapping the mount about to arrive at "a.b".
	// A bare MountClient (no runtime) is enough: the watch is on the base path, so
	// the controller is never asked to serve it.
	w1, err := client.Watch(ctx, "a", waitAbsent)
	if err != nil {
		t.Fatalf("watch before mount: %v", err)
	}

	mc, err := Mount(&MountConfig{DocdAddr: docd.TCPAddr(), Controller: "c", Path: "a.b"})
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitMount(t, docd, "a.b")

	drainUntilClosed(t, w1)
	var ended *WatchEndedError
	if !errors.As(w1.Err(), &ended) || ended.Reason != api.ErrCodeSessionMounted {
		t.Fatalf("mount should end the watch with %q, got %v", api.ErrCodeSessionMounted, w1.Err())
	}

	// Re-watch the same path, now composed over the mount, and take the mount away.
	w2, err := client.Watch(ctx, "a", waitAbsent)
	if err != nil {
		t.Fatalf("re-watch after mount: %v", err)
	}
	short := 50 * time.Millisecond
	if err := mc.Unmount(&short); err != nil {
		t.Fatalf("graceful unmount: %v", err)
	}

	drainUntilClosed(t, w2)
	ended = nil
	if !errors.As(w2.Err(), &ended) || ended.Reason != api.ErrCodeSessionUnmounted {
		t.Fatalf("unmount should end the watch with %q, got %v", api.ErrCodeSessionUnmounted, w2.Err())
	}
}

// TestDocd_WatchUnderMountPathSeesMount covers the other overlap direction. The
// coordinator conflicts a writer at P with a reader that is P's ancestor OR sits
// at/below it, and both are force-ended by the same code path — so a watch NESTED
// under an arriving mount must be told "mounted" too, not left with the ancestor
// case's wording by accident.
func TestDocd_WatchUnderMountPathSeesMount(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdForce(t, logd.TCPAddr(), 50*time.Millisecond)
	client := docdClient(t, docd, "client")

	// "a.b.c" is nested under the mount that is about to register at "a.b".
	w, err := client.Watch(context.Background(), "a.b.c", waitAbsent)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	if _, err := Mount(&MountConfig{DocdAddr: docd.TCPAddr(), Controller: "c", Path: "a.b"}); err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitMount(t, docd, "a.b")

	drainUntilClosed(t, w)
	var ended *WatchEndedError
	if !errors.As(w.Err(), &ended) || ended.Reason != api.ErrCodeSessionMounted {
		t.Fatalf("nested watch should end with %q, got %v", api.ErrCodeSessionMounted, w.Err())
	}
}
