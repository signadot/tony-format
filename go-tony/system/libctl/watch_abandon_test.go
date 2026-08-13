package libctl

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// gatedProxy fronts a real logd so a test can stand in the moment between "the
// server registered the watch" and "the client gave up on it":
//
//   - responses reach the client respDelay late, so a caller's context can expire
//     while its request has really been delivered and acted on;
//   - requests after the first passRequests are held holdRequests before being
//     forwarded, so what the client sends on its way out arrives on the test's
//     terms rather than a microsecond behind the request it follows.
//
// Requests are newline-terminated documents (see sendRequestTo), so counting
// newlines in the client's stream counts requests.
func gatedProxy(t *testing.T, upstream string, respDelay time.Duration, passRequests int, holdRequests time.Duration) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			down, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer down.Close()
				up, err := net.Dial("tcp", upstream)
				if err != nil {
					return
				}
				defer up.Close()

				go func() { // responses, delayed
					buf := make([]byte, 8192)
					for {
						n, err := up.Read(buf)
						if n > 0 {
							time.Sleep(respDelay)
							if _, werr := down.Write(buf[:n]); werr != nil {
								return
							}
						}
						if err != nil {
							return
						}
					}
				}()

				buf := make([]byte, 8192) // requests, held after the first passRequests
				seen := 0
				for {
					n, err := down.Read(buf)
					if n > 0 {
						chunk := bytes.Clone(buf[:n])
						if seen >= passRequests {
							time.Sleep(holdRequests)
						}
						seen += bytes.Count(chunk, []byte{'\n'})
						if _, werr := up.Write(chunk); werr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	return ln.Addr().String()
}

// A caller whose context expires while its watch request is in flight abandons the
// watch — but the request was delivered, so logd has it registered, and the session
// stays up on the same connection: from where the server stands this is a healthy
// client with a watch it does not read, and every commit thereafter fans out to a
// watcher nobody will ever receive from. Nothing but the client can know, so the
// client has to say so.
func TestWatch_AbandonedInFlightIsUnwatched(t *testing.T) {
	srv := startLogd(t)
	// Responses run 400ms late so the 100ms watch below expires with its request
	// already registered; the unwatch that follows is held 600ms so this test can
	// see the watch on the server before it is taken off.
	addr := gatedProxy(t, srv.TCPAddr(), 400*time.Millisecond, 2, 600*time.Millisecond)

	s := NewLogdSession(&LogdSessionConfig{Addr: addr, ClientID: "abandoner"})
	defer s.Close()
	// Connect first, on a context that can afford the delayed hello: the watch below
	// must fail for the one reason under test.
	if err := s.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := s.Watch(ctx, "users/1", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the watch to be abandoned on its deadline, got %v", err)
	}

	if got := srv.Hub.WatcherCount(); got != 1 {
		t.Fatalf("expected logd to be holding the abandoned watch, got %d watchers", got)
	}

	deadline := time.Now().Add(5 * time.Second)
	for srv.Hub.WatcherCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("logd still holds the abandoned watch: nothing ever told it")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
