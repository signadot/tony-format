package txpool

import (
	"context"
	"net"
	"testing"
	"time"
)

// Regression for issue rgn4amsw: Get holds p.mu across the logd round trip, so a stalled /
// half-open logd (TCP alive, never responds) must not wedge Get — and thus every pooled tx
// allocation — forever. A per-round-trip deadline bounds it to an error.
func TestPool_Get_DoesNotWedgeOnStalledLogd(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// Accept connections and hold them open without ever responding (black-hole peer).
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
			held = append(held, c) // keep referenced so it isn't finalized/closed
		}
	}()

	p := New(&Config{LogdAddr: ln.Addr().String(), IOTimeout: 200 * time.Millisecond})
	defer p.Close()

	done := make(chan error, 1)
	go func() {
		_, err := p.Get(context.Background(), 1)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from a stalled logd, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Get wedged on a stalled logd — the round-trip deadline was not enforced")
	}
}
