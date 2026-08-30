package libctl

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// Regression for issue 9zkm8f1y: request() holds s.mu across the request write, so a peer
// that stops READING (TCP alive, send buffer full) must not block the write — and thus s.mu,
// and the heartbeat's own recovery — forever. A write deadline bounds it to an error.
func TestLogdSession_SendRequest_BoundedBySlowPeer(t *testing.T) {
	client, peer := net.Pipe() // net.Pipe writes block until the peer reads
	defer client.Close()
	defer peer.Close()
	// Never read from peer -> Write blocks until the write deadline fires.

	s := &LogdSession{wireTimeout: 200 * time.Millisecond}
	done := make(chan error, 1)
	go func() {
		done <- s.sendRequestTo(context.Background(), client, &api.SessionRequest{Hello: &api.Hello{ClientID: "x"}})
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected the request write to fail on a stalled peer, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sendRequestTo wedged on a slow peer — write deadline not enforced")
	}
}

// The request write is bounded by the CALLER's deadline too, not only the session's wire
// timeout. The writer holds the wire while it runs, so a peer that has stopped reading made
// a caller who asked for a fraction of a second wait out the full wireTimeout -- and every
// other writer queued behind it wait that long as well (residual of ps8kfs9dh12kr777fnn0).
// Fails against the previous code by taking the full wireTimeout.
func TestLogdSession_SendRequest_BoundedByCallerDeadline(t *testing.T) {
	client, peer := net.Pipe() // net.Pipe writes block until the peer reads
	defer client.Close()
	defer peer.Close()
	// Never read from peer -> Write blocks until whichever deadline fires first.

	s := &LogdSession{wireTimeout: 30 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	started := time.Now()
	done := make(chan error, 1)
	go func() {
		done <- s.sendRequestTo(ctx, client, &api.SessionRequest{Hello: &api.Hello{ClientID: "x"}})
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected the request write to fail on a stalled peer, got nil")
		}
		if took := time.Since(started); took > 5*time.Second {
			t.Fatalf("write took %v: bounded by wireTimeout, not by the caller's deadline", took)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the caller's deadline did not bound the request write")
	}
}

// The hello handshake read must also be bounded: a peer that completes the TCP dial but never
// answers hello must not wedge Connect (which holds s.mu) forever.
func TestLogdSession_Connect_BoundedByStalledHello(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
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
			held = append(held, c) // accept, never answer hello
		}
	}()

	s := NewLogdSession(&LogdSessionConfig{Addr: ln.Addr().String(), ClientID: "x", WireTimeout: 200 * time.Millisecond})
	defer s.Close()

	done := make(chan error, 1)
	go func() { done <- s.Connect(context.Background()) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected Connect to fail on a stalled hello, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Connect wedged on a stalled hello — read deadline not enforced")
	}
}
