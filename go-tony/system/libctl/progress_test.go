package libctl

import (
	"context"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// untilProgress reads a watch's events up to the first progress event, and answers the
// patch events ahead of it and the event itself.
func untilProgress(t *testing.T, w *Watch) ([]*api.WatchEvent, *api.WatchEvent) {
	t.Helper()
	var patches []*api.WatchEvent
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-w.Events():
			if !ok {
				t.Fatalf("the watch ended before its progress event: %v", w.Err())
			}
			if ev.Progress {
				return patches, ev
			}
			if ev.Patch != nil {
				patches = append(patches, ev)
			}
		case <-timeout:
			t.Fatal("no progress event in 5s")
		}
	}
}

// Watch.Progress puts a progress event in the watch's Events after every event for a
// commit at or below the head it answers, directly against logd and through docd,
// which passes it to the logd session the watch went to. Most of the writes do not reach
// the watched path: the event is what says the watch is current through them.
func TestWatchProgress(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	for _, tc := range []struct {
		name    string
		session func() *LogdSession
	}{
		{"logd", func() *LogdSession {
			s := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "direct"})
			t.Cleanup(func() { s.Close() })
			return s
		}},
		{"docd", func() *LogdSession { return docdClient(t, docd, "via-docd") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s := tc.session()
			a, b := tc.name+"a", tc.name+"b"
			if _, err := s.Patch(ctx, a, ir.FromInt(0)); err != nil {
				t.Fatal(err)
			}
			w, err := s.Watch(ctx, a, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			if ev := <-w.Events(); ev == nil || ev.State == nil {
				t.Fatalf("first event %+v, want the state", ev)
			}

			var lastA int64
			for i := 1; i <= 5; i++ {
				r, err := s.Patch(ctx, a, ir.FromInt(int64(i)))
				if err != nil {
					t.Fatal(err)
				}
				lastA = r.Commit
				for j := 0; j < 4; j++ {
					if _, err := s.Patch(ctx, b, ir.FromInt(int64(i*10+j))); err != nil {
						t.Fatal(err)
					}
				}
			}
			head, err := w.Progress(ctx)
			if err != nil {
				t.Fatalf("progress: %v", err)
			}
			if head <= lastA {
				t.Errorf("progress answered commit %d, not past the writes to b after %d", head, lastA)
			}
			patches, ev := untilProgress(t, w)
			if ev.Commit < head || ev.Path != a {
				t.Errorf("progress event %+v, want path %s at commit %d or later", ev, a, head)
			}
			if len(patches) != 5 || patches[4].Commit != lastA {
				t.Errorf("%d patch events ahead of the progress event, the last at %v; want 5, the last at %d",
					len(patches), patches, lastA)
			}
		})
	}
}

// A progress request names a watch by its id, and one the session does not hold is
// refused, by docd as by logd.
func TestWatchProgressRefused(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	direct := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "direct"})
	t.Cleanup(func() { direct.Close() })
	ctx := context.Background()
	for name, s := range map[string]*LogdSession{"logd": direct, "docd": docdClient(t, docd, "client")} {
		gone := &Watch{id: "no-such-watch", path: "x", session: s}
		if _, err := gone.Progress(ctx); err == nil {
			t.Errorf("%s answered progress on a watch the session does not hold", name)
		}
	}
}
