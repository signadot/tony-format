package libctl

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
)

// darkAddr is an address that completes the TCP dial and then says nothing — a
// blackholed route, a rolled pod, a server too busy to answer. Connections are held
// open until the test ends, since a peer that CLOSED would be the easy case.
func darkAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		var held []net.Conn
		for {
			c, err := ln.Accept()
			if err != nil {
				for _, h := range held {
					h.Close()
				}
				return
			}
			held = append(held, c)
		}
	}()
	return ln.Addr().String()
}

// A caller's deadline must bound the handshake, not just the wire timeout. The
// handshake read took its own 30s deadline unconditionally, so a caller who asked
// for a fraction of that waited the whole of it against a peer that had accepted
// the connection and gone quiet.
func TestConnect_HandshakeHonoursCallerDeadline(t *testing.T) {
	s := NewLogdSession(&LogdSessionConfig{Addr: darkAddr(t), ClientID: "bounded"})
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := s.Connect(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected Connect to fail against a peer that never answers hello")
	}
	// The wire timeout defaults to 30s; anything near it means the caller's deadline
	// was ignored.
	if elapsed > 3*time.Second {
		t.Errorf("Connect took %v for a 300ms context: the handshake ignored it", elapsed)
	}
}

// A reconnect must not be serialized behind the mutex every request needs. While it
// was, a caller with a ten-second budget did not spend it waiting for the store: it
// spent it queued on a lock that takes no context, was handed the lock after the
// outage, and only then discovered its deadline had gone by — so every caller
// reported `context deadline exceeded` on a request that never reached the wire.
func TestConnect_ReconnectDoesNotStarveOtherCallers(t *testing.T) {
	s := NewLogdSession(&LogdSessionConfig{Addr: darkAddr(t), ClientID: "starved"})
	defer s.Close()

	// One caller reconnecting with all the patience in the world: it will dial, wait
	// out the handshake, back off, and dial again for as long as its context allows.
	holdCtx, holdCancel := context.WithCancel(context.Background())
	defer holdCancel()
	holding := make(chan error, 1)
	go func() {
		_, err := s.Watch(holdCtx, "users/1", nil)
		holding <- err
	}()

	// Give it time to get into the connect it will not come out of.
	time.Sleep(200 * time.Millisecond)
	select {
	case err := <-holding:
		t.Fatalf("the holding watch was meant to still be reconnecting, got %v", err)
	default:
	}

	// A second caller with a budget of its own must get its own answer, on time.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := s.Patch(ctx, "users/2", ir.FromMap(map[string]*ir.Node{"v": ir.FromInt(1)}))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected the patch to fail: nothing is answering")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected the caller's own deadline, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("patch took %v for a 500ms budget: it was queued behind the reconnect", elapsed)
	}

	holdCancel()
	select {
	case <-holding:
	case <-time.After(5 * time.Second):
		t.Error("the holding watch did not come back after its context was cancelled")
	}
}
