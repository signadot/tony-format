package libctl

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func showNode(n *ir.Node) string {
	if n == nil {
		return "<nil>"
	}
	var b bytes.Buffer
	if err := encode.Encode(n, &b, encode.EncodeWire(true)); err != nil {
		return "<unrenderable>"
	}
	return b.String()
}

// A commit seen through a RESUMED watch must be the same delta a live watch saw. It was
// not: the live path publishes a stripped copy, while a replay handed over the stored
// patch with its internal marker still on it, so the same commit arrived as
//
//	live    {verse: {items: {a: !delete {n: 1}}}}
//	replay  {verse: {items: {a: !delete.logd-patch-root {n: 1}}}}
//
// which a consumer testing for !delete reads as an ordinary write -- and, because the extra
// tag makes the folded state differ from the state before it, the gate that suppresses an
// identical write stopped suppressing on the replay path (xmxt2p85h12ksjp1gsn0).
func TestLiveAndReplayDeliverTheSameDeltas(t *testing.T) {
	srv := startLogd(t)
	ctx := context.Background()
	s := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "c"})
	defer s.Close()
	if err := s.Connect(ctx); err != nil {
		t.Fatalf("connect: %s", err)
	}

	if _, err := s.Patch(ctx, "verse.items.a", ir.FromMap(map[string]*ir.Node{"n": ir.FromInt(1)})); err != nil {
		t.Fatalf("seed: %s", err)
	}
	base := s.KnownCommit()

	live, err := s.Watch(ctx, "verse", waitAbsent)
	if err != nil {
		t.Fatalf("live watch: %s", err)
	}
	defer live.Close()
	select {
	case ev := <-live.Events():
		if ev.State == nil {
			t.Fatalf("no initial state: %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no initial state")
	}

	// A write, the SAME write again, and a delete.
	del, err := parse.Parse([]byte(`!delete {n: 1}`))
	if err != nil {
		t.Fatalf("parse delete: %s", err)
	}
	for _, w := range []struct {
		path string
		data *ir.Node
	}{
		{"verse.items.b", ir.FromMap(map[string]*ir.Node{"n": ir.FromInt(2)})},
		{"verse.items.b", ir.FromMap(map[string]*ir.Node{"n": ir.FromInt(2)})},
		{"verse.items.a", del},
	} {
		if _, err := s.Patch(ctx, w.path, w.data); err != nil {
			t.Fatalf("write %s: %s", w.path, err)
		}
	}

	collect := func(w *Watch, label string) []string {
		var out []string
		deadline := time.After(3 * time.Second)
		for {
			select {
			case ev, ok := <-w.Events():
				if !ok {
					return out
				}
				switch {
				case ev.ReplayComplete:
					return out
				case ev.Patch != nil:
					out = append(out, showNode(ev.Patch))
					t.Logf("%s: %s", label, showNode(ev.Patch))
				}
			case <-deadline:
				return out
			}
		}
	}
	liveDeltas := collect(live, "live")

	replay, err := s.Watch(ctx, "verse", &WatchOptions{FromCommit: &base})
	if err != nil {
		t.Fatalf("replay watch: %s", err)
	}
	defer replay.Close()
	replayDeltas := collect(replay, "replay")

	if len(liveDeltas) != len(replayDeltas) {
		t.Fatalf("live delivered %d deltas and a replay of the same commits delivered %d:\n live   %v\n replay %v",
			len(liveDeltas), len(replayDeltas), liveDeltas, replayDeltas)
	}
	for i := range liveDeltas {
		if liveDeltas[i] != replayDeltas[i] {
			t.Errorf("delta %d differs between live and replay\n live   %s\n replay %s",
				i, liveDeltas[i], replayDeltas[i])
		}
	}

	// The properties a consumer depends on, named rather than implied.
	for _, d := range replayDeltas {
		if bytes.Contains([]byte(d), []byte("logd-patch-root")) {
			t.Errorf("an internal marker reached the client: %s", d)
		}
	}
	if len(liveDeltas) != 2 {
		t.Errorf("expected two deltas -- the write and the delete, with the identical rewrite suppressed -- got %d: %v",
			len(liveDeltas), liveDeltas)
	}
}
