package libctl

import (
	"context"
	"testing"
	"time"
)

// An unwatch that reaches docd while its watch is still being admitted -- held
// back by a pending mount under writer priority -- cancels the watch where it
// stands. It used to find nothing to release and pass through, and the watch went
// on to be established after the mount landed: a subscription at the controller
// nobody held, and a reader token a later overlapping writer waited forceAfter on
// (4ynqp7wqh12krg32msn0 item 9).
func TestUnwatchOfAWatchStillInAdmissionCancelsIt(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdForce(t, logd.TCPAddr(), time.Second)

	// A base watch overlapping a.b keeps the coming mount pending for a second.
	holder := docdClient(t, docd, "holder")
	held, err := holder.Watch(context.Background(), "a.b.c", waitAbsent)
	if err != nil {
		t.Fatalf("watch a.b.c: %v", err)
	}
	defer held.Close()

	ctrl := newMemController()
	ctrl.watchable = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = RunController(ctx, &ControllerConfig{DocdAddr: docd.TCPAddr(), Controller: "cB", Path: "a.b", Handler: ctrl})
	}()
	time.Sleep(100 * time.Millisecond) // the mount is now pending behind the held watch

	// A watch under the pending mount waits in admission; its caller gives up first,
	// and libctl sends the unwatch (unwatchAbandoned).
	client := docdClient(t, docd, "client")
	wctx, wcancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer wcancel()
	if _, err := client.Watch(wctx, "a.b.d", waitAbsent); err == nil {
		t.Fatal("the watch was admitted while the mount was pending")
	}

	waitMount(t, docd, "a.b")
	time.Sleep(300 * time.Millisecond) // admission of the abandoned watch would complete here
	if n := ctrl.subCount(); n != 0 {
		t.Fatalf("the controller serves %d watch(es) for a client that unwatched before admission", n)
	}
}
