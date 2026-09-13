package libctl

import (
	"context"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
)

// A watch that failed here as a slow consumer is closed on this side only; the server
// is still serving it. Close on such a watch sends the unwatch, as it does on a live
// one. It returned early on a closed watch and never did, so a consumer that fell
// behind under load left a live stream on the server for every watch it
// re-established (4jhyq24nh12kszvjmsn0; sweep 4ynqp7wq item 10).
func TestCloseAfterSlowConsumerUnwatchesOnTheServer(t *testing.T) {
	srv := startLogd(t)
	sess := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "slow"})
	defer sess.Close()
	ctx := context.Background()
	if _, err := sess.Patch(ctx, "a", ir.FromInt(1)); err != nil {
		t.Fatal(err)
	}

	w, err := sess.Watch(ctx, "a", &WatchOptions{BufferSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	for i := 2; i < 6; i++ { // the consumer reads nothing: the buffer of one fills
		if _, err := sess.Patch(ctx, "a", ir.FromInt(int64(i))); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for w.Err() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the watch did not fail as a slow consumer")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if srv.Hub.WatcherCount() != 1 {
		t.Fatalf("server watchers before Close: %d, want 1", srv.Hub.WatcherCount())
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if n := srv.Hub.WatcherCount(); n != 0 {
		t.Fatalf("server watchers after Close: %d, want 0", n)
	}
	if err := w.Close(); err != nil { // a second Close has nothing more to say
		t.Fatalf("second Close: %v", err)
	}
}
