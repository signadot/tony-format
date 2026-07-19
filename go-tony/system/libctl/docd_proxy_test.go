package libctl

import (
	"context"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	docdserver "github.com/signadot/tony-format/go-tony/system/docd/server"
)

// startDocdProxy starts a docd server with only its client-facing listener
// running, proxying to the given logd address. This is the M1a configuration:
// docd is a transparent pass-through to logd on the client protocol.
func startDocdProxy(t *testing.T, logdAddr string) *docdserver.Server {
	t.Helper()
	srv := docdserver.New(&docdserver.Spec{LogdAddr: logdAddr})
	if err := srv.StartClientTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start docd client listener: %v", err)
	}
	t.Cleanup(func() { srv.StopClientTCP() })
	return srv
}

// TestLogdSession_ThroughDocd is the M1a switchability proof: an unmodified
// LogdSession pointed at docd's client address behaves exactly as if pointed at
// logd. The same Patch/Match/Watch operations round-trip through docd to logd.
func TestLogdSession_ThroughDocd(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdProxy(t, logd.TCPAddr())

	// The client points at docd, not logd — the only change vs. talking to logd
	// directly is the address.
	session := NewLogdSession(&LogdSessionConfig{
		Addr:     docd.ClientTCPAddr(),
		ClientID: "test-client",
	})
	defer session.Close()

	ctx := context.Background()

	// Hello handshake flows through docd; ServerID is logd's, proving transparency.
	if err := session.Connect(ctx); err != nil {
		t.Fatalf("Connect through docd failed: %v", err)
	}
	if session.ServerID() == "" {
		t.Error("expected ServerID (from logd) to be set through docd")
	}

	// Patch then Match round-trip through docd.
	data := ir.FromMap(map[string]*ir.Node{
		"name": ir.FromString("alice"),
	})
	if err := session.Patch(ctx, "users/1", data); err != nil {
		t.Fatalf("Patch through docd failed: %v", err)
	}

	result, err := session.Match(ctx, "users/1")
	if err != nil {
		t.Fatalf("Match through docd failed: %v", err)
	}
	if result.Type != ir.ObjectType {
		t.Fatalf("expected object, got %v", result.Type)
	}
	nameNode, err := result.GetPath("$.name")
	if err != nil {
		t.Fatalf("GetPath failed: %v", err)
	}
	if nameNode == nil || nameNode.String != "alice" {
		t.Errorf("expected name='alice' through docd, got %v", nameNode)
	}
}

// TestLogdSession_WatchThroughDocd proves streaming watch events flow back
// through docd unchanged: docd forwards logd's unsolicited watch pushes.
func TestLogdSession_WatchThroughDocd(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdProxy(t, logd.TCPAddr())

	session := NewLogdSession(&LogdSessionConfig{
		Addr:     docd.ClientTCPAddr(),
		ClientID: "watch-client",
	})
	defer session.Close()

	ctx := context.Background()

	// Seed initial state.
	if err := session.Patch(ctx, "users/1", ir.FromMap(map[string]*ir.Node{
		"name": ir.FromString("alice"),
	})); err != nil {
		t.Fatalf("initial Patch failed: %v", err)
	}

	// Watch through docd; the first event carries the full state.
	w, err := session.Watch(ctx, "users/1", nil)
	if err != nil {
		t.Fatalf("Watch through docd failed: %v", err)
	}
	defer w.Close()

	select {
	case ev, ok := <-w.Events():
		if !ok {
			t.Fatalf("watch closed before initial event: %v", w.Err())
		}
		if ev.State == nil {
			t.Fatalf("expected initial state event, got %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial watch event through docd")
	}

	// Mutate and confirm a subsequent event streams through docd.
	if err := session.Patch(ctx, "users/1", ir.FromMap(map[string]*ir.Node{
		"name": ir.FromString("bob"),
	})); err != nil {
		t.Fatalf("second Patch failed: %v", err)
	}

	select {
	case ev, ok := <-w.Events():
		if !ok {
			t.Fatalf("watch closed before update event: %v", w.Err())
		}
		if ev.Commit == 0 {
			t.Errorf("expected a committed update event, got %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for update watch event through docd")
	}
}
