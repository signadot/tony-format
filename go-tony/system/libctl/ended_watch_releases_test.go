package libctl

import (
	"context"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	docdserver "github.com/signadot/tony-format/go-tony/system/docd/server"
	logdserver "github.com/signadot/tony-format/go-tony/system/logd/server"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// mountUnder mounts a controller at path and answers how long docd took to admit
// it: the time an overlapping watch that still holds its reader token makes it
// wait, up to forceAfter.
func mountUnder(t *testing.T, docd *docdserver.Server, path string) time.Duration {
	t.Helper()
	ctrl := newMemController()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	started := time.Now()
	go func() {
		_ = RunController(ctx, &ControllerConfig{DocdAddr: docd.TCPAddr(), Controller: "under-" + path, Path: path, Handler: ctrl})
	}()
	waitMount(t, docd, path)
	return time.Since(started)
}

// A watch that logd ended, or that was refused when it was asked for, holds nothing
// at docd: its reader token goes back to the mount coordinator when the end passes
// through to the client. Both passed straight through, and every mount overlapping
// such a watch waited forceAfter on it -- forever at "0" -- since the client drops
// the watch on the terminal event and sends no unwatch (jk3s11hxh12ksz5xmdn0).
func TestAnEndedOrRefusedWatchReleasesItsToken(t *testing.T) {
	const forceAfter = 1500 * time.Millisecond
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	logd := logdserver.New(&logdserver.Spec{Storage: store})
	if err := logd.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("start logd: %v", err)
	}
	t.Cleanup(func() { logd.StopTCP() })
	docd := startDocdForce(t, logd.TCPAddr(), forceAfter)
	client := docdClient(t, docd, "client")
	ctx := context.Background()

	// Refused: a watch on a path holding nothing, without waitIfAbsent.
	if _, err := client.Watch(ctx, "nothing.here", nil); err == nil {
		t.Fatal("a watch on nothing was not refused")
	}
	if took := mountUnder(t, docd, "nothing.here.x"); took > forceAfter/2 {
		t.Errorf("a mount under a refused watch took %v: the refused watch kept its token", took)
	}

	// Ended by logd: a replay from a commit compaction has left behind. The watch
	// is confirmed, then ended with replay_compacted before any event.
	for i := 1; i <= 3; i++ {
		if _, err := client.Patch(ctx, "gone", ir.FromInt(int64(i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	cfg := storage.DefaultCompactionConfig()
	cfg.Cutoff = -time.Hour
	if err := store.Compact(cfg); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if store.ReplayFloor() == 0 {
		t.Fatal("compaction left the replay floor at 0")
	}
	from := int64(1)
	w, err := client.Watch(ctx, "gone", &WatchOptions{FromCommit: &from})
	if err != nil {
		t.Fatalf("watch gone from 1: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for w.Err() == nil {
		if time.Now().After(deadline) {
			t.Fatal("logd did not end the watch below the replay floor")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if took := mountUnder(t, docd, "gone.x"); took > forceAfter/2 {
		t.Errorf("a mount under a watch logd ended took %v: the ended watch kept its token", took)
	}
}
