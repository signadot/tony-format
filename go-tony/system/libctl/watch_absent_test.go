package libctl

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A watch on a path holding nothing is refused, and a client that meant to wait says so.
//
// It used to be established silently and deliver null, which is the same thing a read of
// that path used to answer -- so "watch this, it will appear" and "watch this, I have the
// path wrong" were one request with one outcome (bymhrqz7h12ksas3jhn0). Waiting is a real
// thing to want, so it is asked for rather than assumed.
func TestWatchOnAnAbsentPath(t *testing.T) {
	t.Run("refused by default", func(t *testing.T) {
		srv := startLogd(t)
		session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "c"})
		defer session.Close()
		ctx := context.Background()
		if err := session.Connect(ctx); err != nil {
			t.Fatalf("connect: %v", err)
		}

		w, err := session.Watch(ctx, "a.b", nil)
		if err != nil {
			// Refused outright is a fine way to say it too.
			if api.ErrorCode(err) != api.ErrCodeNotFound {
				t.Fatalf("watch: %v, want not_found", err)
			}
			return
		}
		// Or established and then ended with not_found, which is how the server
		// answers once the confirmation is already out.
		deadline := time.After(5 * time.Second)
		for {
			select {
			case _, ok := <-w.Events():
				if !ok {
					// A watch that has already been confirmed says so by ending,
					// which carries the reason rather than a session error.
					var ended *WatchEndedError
					if !errors.As(w.Err(), &ended) || ended.Reason != api.ErrCodeNotFound {
						t.Fatalf("watch ended with %v, want not_found", w.Err())
					}
					return
				}
				t.Fatal("an absent path delivered an event")
			case <-deadline:
				t.Fatal("the watch neither ended nor delivered")
			}
		}
	})

	t.Run("waits when asked", func(t *testing.T) {
		srv := startLogd(t)
		session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "c"})
		defer session.Close()
		ctx := context.Background()
		if err := session.Connect(ctx); err != nil {
			t.Fatalf("connect: %v", err)
		}

		w, err := session.Watch(ctx, "a.b", &WatchOptions{WaitIfAbsent: true})
		if err != nil {
			t.Fatalf("watch with WaitIfAbsent: %v", err)
		}
		// The initial event says there is nothing there.
		select {
		case ev, ok := <-w.Events():
			if !ok {
				t.Fatalf("the watch ended: %v", w.Err())
			}
			if ev.State != nil && ev.State.Type != ir.NullType {
				t.Errorf("initial state = %v, want null for an absent path", ev.State.Type)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no initial event")
		}

		// And the value is reported when it arrives.
		if _, err := session.Patch(ctx, "a.b", ir.FromInt(7)); err != nil {
			t.Fatalf("patch: %v", err)
		}
		select {
		case ev, ok := <-w.Events():
			if !ok {
				t.Fatalf("the watch ended before the value arrived: %v", w.Err())
			}
			if ev.State == nil && ev.Patch == nil {
				t.Error("the arrival carried neither state nor patch")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the value arrived but the watch did not report it")
		}
	})
}

// waitAbsent is what a test means when it watches a path and then writes to it: the value
// is not there yet, and the watch is to report it when it arrives. A watch says so
// explicitly now, since the default is to answer a path holding nothing the way a read
// does (bymhrqz7h12ksas3jhn0).
var waitAbsent = &WatchOptions{WaitIfAbsent: true}
