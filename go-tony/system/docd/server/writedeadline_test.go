package server

import (
	"net"
	"testing"
	"time"

	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// Regression for issue 0tarechx: writeToClient must not block forever on a slow/dead client.
// It is called synchronously from the mount coordinator's force path and composed-watch
// forwarding, so a stuck client would otherwise wedge watch/mount coordination. A write
// deadline bounds it to an error.
func TestClientSession_WriteToClient_BoundedBySlowClient(t *testing.T) {
	client, peer := net.Pipe() // net.Pipe writes block until the peer reads
	defer client.Close()
	defer peer.Close()
	// Never read from peer -> client.Write blocks until the write deadline fires.

	s := &ClientSession{conn: client, writeTimeout: 200 * time.Millisecond, lastSeen: map[string]int64{}}

	done := make(chan error, 1)
	go func() { done <- s.writeToClient(&logdapi.SessionResponse{}) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected writeToClient to fail on a stalled client, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("writeToClient wedged on a slow client — write deadline not enforced")
	}
}
