package libctl

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// TestLogdSession_HeartbeatTearsDownWedgedSession proves the session heartbeat
// rescues a wedged/half-open session: a server that completes the Hello handshake
// but then stops responding (TCP stays alive) would hang every in-flight request
// forever. The heartbeat's unanswered ping must detect this and tear the connection
// down so pending requests fail (and the caller can reconnect).
func TestLogdSession_HeartbeatTearsDownWedgedSession(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// Fake server: answer only Hello, then read-and-drop everything (a wedge).
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		dec, err := stream.NewDecoder(conn, stream.WithBrackets())
		if err != nil {
			return
		}
		for {
			node, err := readDocument(dec)
			if err != nil {
				return
			}
			var req api.SessionRequest
			if err := req.FromTonyIR(node); err != nil {
				return
			}
			if req.Hello != nil {
				resp := &api.SessionResponse{
					ID:     req.ID,
					Result: &api.SessionResult{Hello: &api.HelloResponse{ServerID: "wedge"}},
				}
				data, _ := resp.ToTony(gomap.EncodeWire(true))
				if _, err := conn.Write(append(data, '\n')); err != nil {
					return
				}
			}
			// Everything else (Ping, Match, ...) is silently dropped: the wedge.
		}
	}()

	sess := NewLogdSession(&LogdSessionConfig{
		Addr:              ln.Addr().String(),
		ClientID:          "c",
		HeartbeatInterval: 150 * time.Millisecond,
		HeartbeatTimeout:  150 * time.Millisecond,
	})
	if err := sess.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sess.Close()

	// A request the wedged server never answers. Without the heartbeat this blocks
	// forever; the heartbeat must fail it once it tears the session down.
	done := make(chan error, 1)
	go func() {
		_, e := sess.Match(context.Background(), "x")
		done <- e
	}()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("expected the wedged request to fail after the heartbeat tears down")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request hung: heartbeat did not tear down the wedged session")
	}
}
