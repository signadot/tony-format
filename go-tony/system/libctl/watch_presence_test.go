package libctl

import (
	"context"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// An absent path is watched, delivered, and read back as absent by a client that consults
// nothing but the wire (presence.md; phase 5): the first event says Absent with no state
// standing in for it; a value arriving is a delta the client folds onto nothing; a null
// arriving is a null and not absence; the value leaving says Absent again, with the delta
// that removed it.
func TestAnAbsentPathIsSaidAbsentOnTheWire(t *testing.T) {
	srv := startLogd(t)
	ctx := context.Background()
	session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "w"})
	defer session.Close()
	writer := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "wr"})
	defer writer.Close()

	w, err := session.Watch(ctx, "a.b", waitAbsent)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer w.Close()

	// What the client holds at a.b, stepped by what arrives; nil is absent.
	var held *ir.Node
	step := func(ev *api.WatchEvent) {
		t.Helper()
		switch {
		case ev.State != nil:
			held = ev.State
		case ev.Absent && ev.Patch == nil:
			held = nil
		case ev.Patch != nil:
			base := held
			if base == nil {
				base = ir.Null()
			}
			next, err := api.NextState(base, ev.Patch)
			if err != nil {
				t.Fatalf("apply: %v", err)
			}
			if ev.Absent {
				held = nil
			} else {
				held = next
			}
		}
	}

	init := expectEvent(t, w)
	if !init.Absent || init.State != nil {
		t.Fatalf("the first event of a watch on an absent path: %+v, want Absent with no state", init)
	}
	step(init)

	if _, err := writer.Patch(ctx, "a.b", vObj(1)); err != nil {
		t.Fatal(err)
	}
	ev := expectEvent(t, w)
	step(ev)
	if ev.Absent || held == nil {
		t.Fatalf("a value arrived and the event says %+v; held %v", ev, held)
	}
	if v, _ := held.GetPath("$.v"); v == nil || v.Int64 == nil || *v.Int64 != 1 {
		t.Errorf("held after the value arrived: %s", encNode(held))
	}

	// A null is a value.
	if _, err := writer.Patch(ctx, "a", ir.FromMap(map[string]*ir.Node{"b": ir.Null()})); err != nil {
		t.Fatal(err)
	}
	ev = expectEvent(t, w)
	step(ev)
	if ev.Absent || held == nil || held.Type != ir.NullType {
		t.Fatalf("a null arrived and the event says %+v; held %v", ev, held)
	}

	// The value leaves: Absent, with the delta that removed it.
	if _, err := writer.Patch(ctx, "a", ir.FromMap(map[string]*ir.Node{"b": ir.Null().WithTag("!delete")})); err != nil {
		t.Fatal(err)
	}
	ev = expectEvent(t, w)
	step(ev)
	if !ev.Absent || ev.Patch == nil || held != nil {
		t.Fatalf("the value left and the event says %+v; held %v", ev, held)
	}
	if _, err := session.Match(ctx, "a.b"); api.ErrorCode(err) != api.ErrCodeNotFound {
		t.Errorf("a read of the absent path answers %v, want not_found", err)
	}
}

// ONE ROOTING: a watch's deltas are rooted at the watched path, as its state event is, so
// a client applies each to what it holds and lands where a fresh read lands -- through
// writes below the path, at it, and above it (an operator above the path replaces what is
// there) -- and a watch that replays the same commits is handed the same deltas, byte for
// byte (one_delta_shape.md; rg5nd1psh12kse7dddn0).
func TestAWatchsDeltasAreRootedAtItsPath(t *testing.T) {
	srv := startLogd(t)
	ctx := context.Background()
	session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "w"})
	defer session.Close()
	writer := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "wr"})
	defer writer.Close()

	seed, err := writer.Patch(ctx, "", mustParseLibctl(t, `{a: {b: {c: 1, d: {e: 2}}, x: 3}, f: 4}`))
	if err != nil {
		t.Fatal(err)
	}
	w, err := session.Watch(ctx, "a.b", nil)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer w.Close()
	init := expectEvent(t, w)
	if init.State == nil {
		t.Fatalf("no initial state: %+v", init)
	}
	held := init.State

	writes := []struct{ path, src string }{
		{"a.b.c", "10"},                 // below the path
		{"a.b", `{g: {h: 5}}`},          // at the path
		{"a.x", "30"},                   // beside it: no event
		{"a.b.d", "# why\n{e: 20}"},     // a comment travels
		{"a", `!raw {b: {only: true}}`}, // an operator above the path
		{"a.b.only", "!delete"},         // and below it again
	}
	var live []string
	for _, wr := range writes {
		if _, err := writer.Patch(ctx, wr.path, mustParseLibctl(t, wr.src)); err != nil {
			t.Fatalf("write %s: %v", wr.path, err)
		}
		if wr.path == "a.x" {
			expectQuiet(t, "a.b", w)
			continue
		}
		ev := expectEvent(t, w)
		if ev.Patch == nil || ev.Path != "a.b" {
			t.Fatalf("write %s: %+v", wr.path, ev)
		}
		live = append(live, encNode(ev.Patch))
		base := held
		if base == nil {
			base = ir.Null()
		}
		next, err := api.NextState(base, ev.Patch)
		if err != nil {
			t.Fatalf("apply after %s: %v", wr.path, err)
		}
		held = next
		if ev.Absent {
			held = nil
		}
		fresh, err := session.Match(ctx, "a.b")
		if err != nil {
			t.Fatalf("read after %s: %v", wr.path, err)
		}
		if !api.SameState(held, fresh) {
			t.Fatalf("after %s <- %s the applied delta and a fresh read differ\n applied %s\n fresh   %s",
				wr.path, wr.src, encNode(held), encNode(fresh))
		}
	}

	// The same commits replayed are the same bytes.
	from := seed.Commit
	replay, err := session.Watch(ctx, "a.b", &WatchOptions{FromCommit: &from, NoInit: true})
	if err != nil {
		t.Fatalf("replay watch: %v", err)
	}
	defer replay.Close()
	var got []string
	for len(got) < len(live) {
		ev := expectEvent(t, replay)
		if ev.ReplayComplete {
			break
		}
		if ev.Patch != nil {
			got = append(got, encNode(ev.Patch))
		}
	}
	if len(got) != len(live) {
		t.Fatalf("replay delivered %d deltas, live %d", len(got), len(live))
	}
	for i := range live {
		if got[i] != live[i] {
			t.Errorf("delta %d differs between live and replay\n live   %s\n replay %s", i, live[i], got[i])
		}
	}
}

func mustParseLibctl(t *testing.T, src string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(src), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return n
}
