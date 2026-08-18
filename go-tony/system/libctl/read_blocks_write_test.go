package libctl

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	logdserver "github.com/signadot/tony-format/go-tony/system/logd/server"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// One client is one session: libctl dials once and every read, write and watch shares
// it. logd's request loop ran each request to completion before reading the next, so a
// read of a big document put every write behind it in the same line -- a source trying
// to land a write waited on a status read of a document it had no interest in, and at
// three seconds a queue of four takes a write past its budget and turns "slow" into
// "dropped, retried, slower" (7qayp3hah12kscx2gdn0).
//
// A slow read must not block a write.
func TestSlowReadDoesNotBlockAWrite(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.Open(dir, nil)
	if err != nil {
		t.Fatalf("open storage: %s", err)
	}
	t.Cleanup(func() { store.Close() })

	// big enough that a root read is unmistakably slower than a write
	blob := strings.Repeat("x", 400)
	ctx := context.Background()
	srv := logdserver.New(&logdserver.Spec{Storage: store})
	if err := srv.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("start logd: %s", err)
	}
	t.Cleanup(func() { srv.StopTCP() })

	seed := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "seed"})
	defer seed.Close()
	if err := seed.Connect(ctx); err != nil {
		t.Fatalf("connect seed: %s", err)
	}
	for i := 0; i < 1500; i++ {
		id := "e" + strconv.Itoa(i)
		_, err := seed.Patch(ctx, "verse.entities."+id, ir.FromMap(map[string]*ir.Node{
			"id": ir.FromString(id), "blob": ir.FromString(blob),
		}))
		if err != nil {
			t.Fatalf("seed write: %s", err)
		}
	}

	// How slow a root read is on this store, and how quick a write is, measured
	// rather than assumed -- the bound below is derived from them.
	session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "verse"})
	defer session.Close()
	if err := session.Connect(ctx); err != nil {
		t.Fatalf("connect: %s", err)
	}
	start := time.Now()
	if _, err := session.Match(ctx, ""); err != nil {
		t.Fatalf("read: %s", err)
	}
	readTook := time.Since(start)
	start = time.Now()
	if _, err := session.Patch(ctx, "verse.meta", ir.FromMap(map[string]*ir.Node{"rev": ir.FromInt(1)})); err != nil {
		t.Fatalf("write: %s", err)
	}
	writeTook := time.Since(start)
	t.Logf("alone: root read %s, write %s", readTook.Round(time.Millisecond), writeTook.Round(time.Millisecond))
	if readTook < 10*writeTook {
		t.Skipf("a root read (%s) is not slow enough against a write (%s) here to tell the two cases apart",
			readTook, writeTook)
	}

	// Now both at once, on the ONE session, the read first.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := session.Match(ctx, ""); err != nil {
			t.Errorf("concurrent read: %s", err)
		}
	}()
	time.Sleep(10 * time.Millisecond) // let the read reach the server first

	start = time.Now()
	if _, err := session.Patch(ctx, "verse.meta", ir.FromMap(map[string]*ir.Node{"rev": ir.FromInt(2)})); err != nil {
		t.Fatalf("write behind a read: %s", err)
	}
	behind := time.Since(start)
	wg.Wait()

	// Serialized, the write waits out the read. Not serialized, it costs what a write
	// costs. Half the read is a bound neither a busy machine nor a fast one confuses.
	t.Logf("behind a read: write %s (read alone %s)", behind.Round(time.Millisecond), readTook.Round(time.Millisecond))
	if behind > readTook/2 {
		t.Errorf("a write behind a slow read took %s; the read alone takes %s, so it queued",
			behind.Round(time.Millisecond), readTook.Round(time.Millisecond))
	}
}

// Reads run off the request loop, so their answers can arrive in any order -- but each
// answer must still reach the request that asked for it. Several reads of different
// paths in flight at once, each checked against what it asked for.
func TestConcurrentReadsAnswerTheRightRequests(t *testing.T) {
	srv := startLogd(t)
	ctx := context.Background()
	session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "many-reads"})
	defer session.Close()
	if err := session.Connect(ctx); err != nil {
		t.Fatalf("connect: %s", err)
	}

	const n = 24
	for i := 0; i < n; i++ {
		id := "e" + strconv.Itoa(i)
		if _, err := session.Patch(ctx, "verse.entities."+id, ir.FromMap(map[string]*ir.Node{
			"id": ir.FromString(id),
		})); err != nil {
			t.Fatalf("write: %s", err)
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := "e" + strconv.Itoa(i)
			got, err := session.Match(ctx, "verse.entities."+id)
			if err != nil {
				t.Errorf("read %s: %s", id, err)
				return
			}
			field, err := got.GetKPath("id")
			if err != nil || field == nil {
				t.Errorf("read %s: no id in %v", id, got)
				return
			}
			if field.String != id {
				t.Errorf("the read for %s answered with %s", id, field.String)
			}
		}()
	}
	wg.Wait()
}
