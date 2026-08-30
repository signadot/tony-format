package libctl

import (
	"context"
	"fmt"
	"testing"
	"time"

	"bytes"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// Paths here are logd kpaths: "." separates segments, so "doc.extra" is a child of "doc".
// A slash is just a character in a field name (the "users/1" seen elsewhere in these
// tests is one flat key, not a two-segment path).

// A baseline watch steps its document forward one patch per commit rather than
// rebuilding it from the last snapshot. The property that has to survive is the one the
// watch exists for: applying every delivered delta, in order, reproduces the server's
// state exactly. Verified by APPLYING the deltas, not by inspecting them.
func TestWatchStepping_DeltasReproduceServerState(t *testing.T) {
	srv := startLogd(t)
	session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "stepper"})
	defer session.Close()
	ctx := context.Background()

	seed, _ := parse.Parse([]byte(`{doc: {n: 0, keep: "base"}}`))
	if _, err := session.Patch(ctx, "", seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Watched at the root, because a watch's state event is rooted at the WATCHED PATH
	// while its patch events are rooted at the DOCUMENT. At the root the two agree, so
	// the deltas can be applied to the state directly, which is the property under test.
	w, err := session.Watch(ctx, "", waitAbsent)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Close()

	first := recvEvent(t, w, 3*time.Second)
	if first.State == nil {
		t.Fatalf("expected an initial state event, got %+v", first)
	}
	doc := first.State

	// Plain merges, a write below the watched path, a delete, and a rewrite of the
	// deleted key -- the shapes that make a stepped document diverge if the fold is wrong.
	writes := []struct{ path, src string }{
		{"doc", `{n: 1}`},
		{"doc", `{extra: {deep: true}}`},
		{"doc.extra.deep", `false`},
		{"doc.extra", `!delete`},
		{"doc", `{n: 2, back: "again"}`},
		{"doc.back", `"changed"`},
		{"doc", `{n: 3}`},
	}
	for i, wr := range writes {
		data, err := parse.Parse([]byte(wr.src))
		if err != nil {
			t.Fatalf("parse %d: %v", i, err)
		}
		if _, err := session.Patch(ctx, wr.path, data); err != nil {
			t.Fatalf("patch %d (%s at %q): %v", i, wr.src, wr.path, err)
		}
	}

	for i := range writes {
		var ev *api.WatchEvent
		select {
		case e, ok := <-w.Events():
			if !ok {
				t.Fatalf("watch closed after %d/%d events: %v", i, len(writes), w.Err())
			}
			ev = e
		case <-time.After(3 * time.Second):
			t.Fatalf("got only %d/%d events; missing the delta for write %d (%s at %q)",
				i, len(writes), i, writes[i].src, writes[i].path)
		}
		if ev.Patch == nil {
			t.Fatalf("expected a patch event, got %+v", ev)
		}
		next, err := tony.Patch(doc, ev.Patch)
		if err != nil {
			t.Fatalf("applying delta at commit %d: %v", ev.Commit, err)
		}
		doc = next
	}

	fresh, err := session.Match(ctx, "")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if !doc.DeepEqual(fresh) {
		t.Errorf("client state from applied deltas differs from a fresh read\n deltas: %s\n  fresh: %s", encNode(doc), encNode(fresh))
	}
}

// The change gate must still suppress a commit under a SHARED ANCESTOR that does not
// reach the watched subtree — the case the gate exists for — now that the subtree comes
// from a stepped document rather than a fresh read.
func TestWatchStepping_SiblingWritesAreStillSuppressed(t *testing.T) {
	srv := startLogd(t)
	session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "gate"})
	defer session.Close()
	ctx := context.Background()

	seed, _ := parse.Parse([]byte(`{mine: {v: 0}, other: {v: 0}}`))
	if _, err := session.Patch(ctx, "users", seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Watch a subtree whose SIBLING shares the ancestor "users", so a write to the
	// sibling wakes this watcher through the coarse top-level KPath match.
	w, err := session.Watch(ctx, "users.mine", &WatchOptions{NoInit: true})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Close()

	for i := range 3 {
		data, _ := parse.Parse(fmt.Appendf(nil, `{v: %d}`, i+1))
		if _, err := session.Patch(ctx, "users.other", data); err != nil {
			t.Fatalf("sibling patch %d: %v", i, err)
		}
	}

	// Nothing may have been delivered for the sibling writes.
	select {
	case ev, ok := <-w.Events():
		if ok {
			t.Fatalf("sibling write under the shared ancestor leaked an event: %+v", ev)
		}
		t.Fatalf("watch closed unexpectedly: %v", w.Err())
	case <-time.After(400 * time.Millisecond):
	}

	// A write that does reach the watched subtree must still arrive.
	mine, _ := parse.Parse([]byte(`{v: 99}`))
	if _, err := session.Patch(ctx, "users.mine", mine); err != nil {
		t.Fatalf("own patch: %v", err)
	}
	ev := recvEvent(t, w, 3*time.Second)
	if ev.Patch == nil {
		t.Fatalf("expected a patch event for the watched path, got %+v", ev)
	}
}

func encNode(n *ir.Node) string {
	if n == nil {
		return "<nil>"
	}
	var b bytes.Buffer
	if err := encode.Encode(n, &b); err != nil {
		return "<encode error>"
	}
	return b.String()
}
