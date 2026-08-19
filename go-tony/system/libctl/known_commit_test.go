package libctl

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
)

// A client wanting to know where the store is should not have to open a watch to be
// told. It had to: the commit was on every match answer and this package dropped it,
// so a caller's only route to a revision was a watch, whose initial state costs a full
// read and reports where the WATCH starts (7qayp3hah12kscx2gdn0).
//
// Now every answer that carries a commit raises the session's mark: reads, writes, and
// -- while nothing is being read -- the heartbeat's pong.
func TestKnownCommitTracksWithoutAWatch(t *testing.T) {
	srv := startLogd(t)
	session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "rev-client"})
	defer session.Close()

	ctx := context.Background()
	if err := session.Connect(ctx); err != nil {
		t.Fatalf("connect: %s", err)
	}

	// A write says where it landed.
	res, err := session.Patch(ctx, "verse.meta", ir.FromMap(map[string]*ir.Node{"rev": ir.FromInt(1)}))
	if err != nil {
		t.Fatalf("patch: %s", err)
	}
	if got := session.KnownCommit(); got != res.Commit {
		t.Errorf("after a write, KnownCommit is %d, want the write's commit %d", got, res.Commit)
	}

	// A read says where the store is, and hands the same number back.
	body, commit, err := session.MatchCommit(ctx, "verse.meta")
	if err != nil {
		t.Fatalf("match: %s", err)
	}
	if body == nil {
		t.Fatal("read nothing at verse.meta")
	}
	if commit < res.Commit {
		t.Errorf("the read reports commit %d, below the write's %d", commit, res.Commit)
	}
	if got := session.KnownCommit(); got != commit {
		t.Errorf("after a read, KnownCommit is %d, want %d", got, commit)
	}

	// Writes by somebody else are the case a watch was being held open for. Another
	// session's commits reach this one on the answers it already gets.
	other := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "other-client"})
	defer other.Close()
	if err := other.Connect(ctx); err != nil {
		t.Fatalf("connect other: %s", err)
	}
	var last int64
	for i := 0; i < 3; i++ {
		r, err := other.Patch(ctx, "verse.meta", ir.FromMap(map[string]*ir.Node{"rev": ir.FromInt(int64(i + 2))}))
		if err != nil {
			t.Fatalf("other patch: %s", err)
		}
		last = r.Commit
	}
	if got := session.KnownCommit(); got >= last {
		t.Fatalf("this session already knows %d without asking; the test proves nothing", got)
	}
	if _, commit, err = session.MatchCommit(ctx, "verse.meta"); err != nil {
		t.Fatalf("match: %s", err)
	}
	if commit != last || session.KnownCommit() != last {
		t.Errorf("after the other session's writes: read %d, known %d, want %d",
			commit, session.KnownCommit(), last)
	}
}

// The heartbeat is what keeps the mark current on a session which is doing nothing --
// the case a poller or a held watch existed to cover.
func TestPongCarriesTheHead(t *testing.T) {
	srv := startLogd(t)
	idle := NewLogdSession(&LogdSessionConfig{
		Addr:              srv.TCPAddr(),
		ClientID:          "idle-client",
		HeartbeatInterval: 50 * time.Millisecond,
	})
	defer idle.Close()

	ctx := context.Background()
	if err := idle.Connect(ctx); err != nil {
		t.Fatalf("connect: %s", err)
	}

	writer := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "writer"})
	defer writer.Close()
	if err := writer.Connect(ctx); err != nil {
		t.Fatalf("connect writer: %s", err)
	}
	res, err := writer.Patch(ctx, "verse.meta", ir.FromMap(map[string]*ir.Node{"rev": ir.FromInt(7)}))
	if err != nil {
		t.Fatalf("patch: %s", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if idle.KnownCommit() >= res.Commit {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("an idle session never learned of commit %d; KnownCommit is %d", res.Commit, idle.KnownCommit())
}

// docd answers the ping itself -- it never forwards it -- so the number it reports is
// its own: the highest commit it has told any client about, over every session. That
// is a revision to compare, not a commit to read at, because docd composes mounts with
// independent commit sequences. What matters here is that it moves, and that a client
// through docd is not left with nothing.
func TestKnownCommitThroughDocd(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdProxy(t, logd.TCPAddr())

	ctx := context.Background()
	session := NewLogdSession(&LogdSessionConfig{
		Addr:              docd.ClientTCPAddr(),
		ClientID:          "docd-rev-client",
		HeartbeatInterval: 50 * time.Millisecond,
	})
	defer session.Close()
	if err := session.Connect(ctx); err != nil {
		t.Fatalf("connect: %s", err)
	}

	res, err := session.Patch(ctx, "verse.meta", ir.FromMap(map[string]*ir.Node{"rev": ir.FromInt(1)}))
	if err != nil {
		t.Fatalf("patch: %s", err)
	}
	if got := session.KnownCommit(); got != res.Commit {
		t.Errorf("after a write through docd, KnownCommit is %d, want %d", got, res.Commit)
	}

	// A fresh session, which has asked for nothing, learns it from the heartbeat.
	idle := NewLogdSession(&LogdSessionConfig{
		Addr:              docd.ClientTCPAddr(),
		ClientID:          "docd-idle-client",
		HeartbeatInterval: 50 * time.Millisecond,
	})
	defer idle.Close()
	if err := idle.Connect(ctx); err != nil {
		t.Fatalf("connect idle: %s", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if idle.KnownCommit() >= res.Commit {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("an idle session through docd never learned of commit %d; KnownCommit is %d",
		res.Commit, idle.KnownCommit())
}

// A caller can ask for a window of history without knowing where the store is: a
// negative FromCommit is relative to the server's watermark, and the watch reports which
// commit that resolved to.
func TestRelativeWatchWindowThroughLibctl(t *testing.T) {
	srv := startLogd(t)
	ctx := context.Background()
	session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "window"})
	defer session.Close()
	if err := session.Connect(ctx); err != nil {
		t.Fatalf("connect: %s", err)
	}

	for i := 0; i < 10; i++ {
		if _, err := session.Patch(ctx, "verse.entities.e"+strconv.Itoa(i),
			ir.FromMap(map[string]*ir.Node{"n": ir.FromInt(int64(i))})); err != nil {
			t.Fatalf("write: %s", err)
		}
	}
	head := session.KnownCommit()

	back := int64(-3)
	w, err := session.Watch(ctx, "verse.entities", &WatchOptions{FromCommit: &back})
	if err != nil {
		t.Fatalf("watch: %s", err)
	}
	defer w.Close()

	from := w.ReplayingFrom()
	if from == nil {
		t.Fatal("a relative watch did not report where it starts")
	}
	if *from != head-3 {
		t.Errorf("replaying from %d, want %d (head %d)", *from, head-3, head)
	}
	if to := w.ReplayingTo(); to == nil || *to < head {
		t.Errorf("replaying to %v, want at least %d", to, head)
	}

	// It replays: the events arriving before replay-complete cover the window asked for.
	deadline := time.After(3 * time.Second)
	seen := 0
	for {
		select {
		case ev, ok := <-w.Events():
			if !ok {
				t.Fatalf("watch closed after %d events: %v", seen, w.Err())
			}
			if ev.Commit > 0 && ev.Commit < head-3 {
				t.Errorf("an event at commit %d is older than the window (%d)", ev.Commit, head-3)
			}
			seen++
			if ev.ReplayComplete {
				if seen < 2 {
					t.Errorf("replay complete after %d events; the window replayed nothing", seen)
				}
				return
			}
		case <-deadline:
			t.Fatalf("no replay-complete after %d events", seen)
		}
	}
}
