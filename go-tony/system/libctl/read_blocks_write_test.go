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
// A ping is the other thing behind it. Its answer is the session's heartbeat, and a ping
// that queued behind a burst of watch opens, each reading its path on the loop, or behind
// a ninth match waiting on the loop for a read slot, timed out and tore the session down
// with every watch on it (x32pp5j0h12krtnvwnn0).
//
// A slow read must not block a write or a ping, however many reads are ahead of it.
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

	connect := func(t *testing.T, name string) *LogdSession {
		t.Helper()
		session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: name})
		t.Cleanup(func() { session.Close() })
		if err := session.Connect(ctx); err != nil {
			t.Fatalf("connect: %s", err)
		}
		return session
	}

	rev := int64(0)
	write := func(session *LogdSession) error {
		rev++
		_, err := session.Patch(ctx, "verse.meta", ir.FromMap(map[string]*ir.Node{"rev": ir.FromInt(rev)}))
		return err
	}
	ping := func(session *LogdSession) error {
		_, err := session.Ping(ctx)
		return err
	}
	match := func(session *LogdSession) error {
		_, err := session.Match(ctx, "")
		return err
	}
	// A watch open without waitIfAbsent asks whether the path holds anything, and one
	// without noInit takes its opening picture: a read of the path either way.
	watch := func(session *LogdSession) error {
		w, err := session.Watch(ctx, "", nil)
		if err != nil {
			return err
		}
		return w.Close()
	}

	// How slow a root read is on this store, and how quick a write and a ping are,
	// measured rather than assumed -- the bound below is derived from them.
	session := connect(t, "verse")
	timed := func(op func(*LogdSession) error) time.Duration {
		start := time.Now()
		if err := op(session); err != nil {
			t.Fatalf("alone: %s", err)
		}
		return time.Since(start)
	}
	readTook, writeTook, pingTook := timed(match), timed(write), timed(ping)
	t.Logf("alone: root read %s, write %s, ping %s", readTook.Round(time.Millisecond),
		writeTook.Round(time.Millisecond), pingTook.Round(time.Millisecond))
	if readTook < 10*max(writeTook, pingTook) {
		t.Skipf("a root read (%s) is not slow enough against a write (%s) or a ping (%s) here to tell the two cases apart",
			readTook, writeTook, pingTook)
	}

	for _, tc := range []struct {
		name   string
		ahead  func(*LogdSession) error
		n      int
		behind func(*LogdSession) error
	}{
		{"write behind a match", match, 1, write},
		{"ping behind a match", match, 1, ping},
		// One more than a session's read slots: the last waits for one.
		{"write behind nine matches", match, 9, write},
		{"ping behind nine matches", match, 9, ping},
		{"write behind nine watch opens", watch, 9, write},
		{"ping behind nine watch opens", watch, 9, ping},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Both at once, on the ONE session, the reads first.
			session := connect(t, "verse")
			var wg sync.WaitGroup
			for range tc.n {
				wg.Go(func() {
					if err := tc.ahead(session); err != nil {
						t.Errorf("read ahead: %s", err)
					}
				})
			}
			time.Sleep(10 * time.Millisecond) // let the reads reach the server first

			start := time.Now()
			if err := tc.behind(session); err != nil {
				t.Fatalf("behind the reads: %s", err)
			}
			behind := time.Since(start)
			wg.Wait()

			// Serialized, what is behind waits out a read. Not serialized, it costs what
			// it costs. Half a read is a bound neither a busy machine nor a fast one
			// confuses.
			t.Logf("behind %d: %s (read alone %s)", tc.n, behind.Round(time.Millisecond), readTook.Round(time.Millisecond))
			if behind > readTook/2 {
				t.Errorf("behind %d slow reads took %s; a read alone takes %s, so it queued",
					tc.n, behind.Round(time.Millisecond), readTook.Round(time.Millisecond))
			}
		})
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
