package server

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/docd/api"
	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// fixedClock builds a clock whose "now" is pinned, so value() is deterministic.
func fixedClock(path string, freq time.Duration, epoch int64, start, now time.Time) *clock {
	return &clock{path: path, freq: freq, epoch: epoch, start: start, now: func() time.Time { return now }}
}

// TestClock_ValueAtTick verifies the clock arithmetic: value = epoch + N*freq(ns)
// where N is the number of whole frequencies elapsed, and pre-start reads clamp to
// tick 0.
func TestClock_ValueAtTick(t *testing.T) {
	start := time.Unix(1000, 0)
	freq := 10 * time.Millisecond
	epoch := int64(100)

	cases := []struct {
		name string
		at   time.Time
		want int64
	}{
		{"tick0 exactly at start", start, 100},
		{"before start clamps to tick0", start.Add(-time.Hour), 100},
		{"mid-first-tick still tick0", start.Add(9 * time.Millisecond), 100},
		{"tick1", start.Add(10 * time.Millisecond), 100 + freq.Nanoseconds()},
		{"tick3 (35ms floors to 3)", start.Add(35 * time.Millisecond), 100 + 3*freq.Nanoseconds()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := fixedClock("sys/clock", freq, epoch, start, tc.at)
			if got := c.value(); got != tc.want {
				t.Errorf("value() = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestNewClock_Validation covers the spec checks (path, frequency).
func TestNewClock_Validation(t *testing.T) {
	start := time.Unix(0, 0)
	bad := []api.ClockSpec{
		{Path: "", Frequency: "1s"},
		{Path: ".meta/x", Frequency: "1s"},
		{Path: "sys/clock", Frequency: "notaduration"},
		{Path: "sys/clock", Frequency: "0"},
		{Path: "sys/clock", Frequency: "-1s"},
	}
	for _, spec := range bad {
		if _, err := newClock(&spec, start); err == nil {
			t.Errorf("newClock(%+v) = nil error, want error", spec)
		}
	}
	if _, err := newClock(&api.ClockSpec{Path: "sys/clock", Frequency: "1s", Epoch: 5}, start); err != nil {
		t.Errorf("newClock(valid) = %v, want nil", err)
	}
}

// TestClockRegistry verifies register/lookup/unregister and the duplicate-path
// rejection, plus that unregister only drops the clock it still owns.
func TestClockRegistry(t *testing.T) {
	r := newClockRegistry()
	start := time.Unix(0, 0)
	c1 := fixedClock("sys/clock", time.Second, 0, start, start)

	if err := r.register(c1); err != nil {
		t.Fatalf("register: %v", err)
	}
	if r.lookup("sys/clock") != c1 {
		t.Fatal("lookup did not return the registered clock")
	}
	dup := fixedClock("sys/clock", time.Second, 0, start, start)
	if err := r.register(dup); err == nil {
		t.Fatal("expected duplicate-path registration to be rejected")
	}

	// A stale unregister of a superseded clock must not drop the live one.
	r.unregister(dup)
	if r.lookup("sys/clock") != c1 {
		t.Fatal("stale unregister dropped the live clock")
	}
	r.unregister(c1)
	if r.lookup("sys/clock") != nil {
		t.Fatal("unregister did not remove the clock")
	}
}

// TestClocksDoc verifies the .meta/clocks document records path/frequency/epoch.
func TestClocksDoc(t *testing.T) {
	start := time.Unix(0, 0)
	doc := clocksDoc([]*clock{
		fixedClock("sys/clock", time.Second, 42, start, start),
	})
	epoch, ok := intAt(t, doc, "$.clocks[0].epoch")
	if !ok || epoch != 42 {
		t.Errorf("epoch = %d (ok=%v), want 42", epoch, ok)
	}
	freq, _ := doc.GetPath("$.clocks[0].frequency")
	if freq == nil || freq.String != "1s" {
		t.Errorf("frequency = %v, want 1s", freq)
	}
	path, _ := doc.GetPath("$.clocks[0].path")
	if path == nil || path.String != "sys/clock" {
		t.Errorf("path = %v, want sys/clock", path)
	}
}

// TestMountHandshake_ClockRegisters drives the mount protocol: a hello carrying a
// clock spec registers a docd-driven clock (no controller mount) and is
// acknowledged, and .meta/clocks records epoch/frequency for retrieval.
func TestMountHandshake_ClockRegisters(t *testing.T) {
	server := New(&Spec{})
	if err := server.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start TCP: %v", err)
	}
	defer server.StopTCP()

	conn, err := net.Dial("tcp", server.TCPAddr())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	req := `{hello: {controller: "clockd", clock: {path: "sys/clock", frequency: "1s", epoch: 7}}}` + "\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	var resp api.MountResponse
	if err := resp.FromTony(bytes.TrimSpace(buf[:n])); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("clock mount failed: %s: %s", resp.Error.Code, resp.Error.Message)
	}
	if resp.Result == nil || resp.Result.Mount == nil || resp.Result.Mount.Path != "sys/clock" {
		t.Fatalf("unexpected mount result: %+v", resp.Result)
	}

	clk := server.Clocks.lookup("sys/clock")
	if clk == nil {
		t.Fatal("clock not registered")
	}
	if clk.freq != time.Second || clk.epoch != 7 {
		t.Errorf("clock freq/epoch = %v/%d, want 1s/7", clk.freq, clk.epoch)
	}
	// It must not have created a controller mount.
	if server.Mounts.Lookup("sys/clock") != nil {
		t.Error("clock hello should not create a controller mount")
	}

	// Disconnect removes the clock outright (no tombstone — nothing to serve).
	conn.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if server.Clocks.lookup("sys/clock") == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("expected clock to be removed after disconnect")
}

// newTestClientSession wires a ClientSession onto conn with the maps and channels
// the clock-serving paths touch (no logd connection needed — clocks are served
// directly by docd).
func newTestClientSession(server *Server, conn net.Conn) *ClientSession {
	return &ClientSession{
		conn:         conn,
		server:       server,
		clockWatches: make(map[string]*clockWatcher),
		lastSeen:     make(map[string]int64),
		done:         make(chan struct{}),
		writeTimeout: time.Second,
	}
}

// readResp decodes one SessionResponse document off conn.
func readResp(t *testing.T, dec *stream.Decoder) *logdapi.SessionResponse {
	t.Helper()
	node, err := decodeDocument(dec)
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var resp logdapi.SessionResponse
	if err := resp.FromTonyIR(node); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	return &resp
}

// TestServeClockMatch verifies a MATCH on a clock path returns the current int
// value directly from docd.
func TestServeClockMatch(t *testing.T) {
	start := time.Unix(1000, 0)
	freq := 10 * time.Millisecond
	// now = start + 35ms -> tick 3 -> value = 100 + 3*10ms(ns)
	clk := fixedClock("sys/clock", freq, 100, start, start.Add(35*time.Millisecond))
	want := int64(100 + 3*freq.Nanoseconds())

	server := New(&Spec{})
	if err := server.Clocks.register(clk); err != nil {
		t.Fatal(err)
	}

	client, peer := net.Pipe()
	defer client.Close()
	defer peer.Close()
	s := newTestClientSession(server, client)
	dec, _ := stream.NewDecoder(peer, stream.WithBrackets())

	req := &logdapi.SessionRequest{Match: &logdapi.MatchRequest{Body: logdapi.PathData{Path: "sys/clock"}}}
	go s.serveClockMatch(req, clk)

	resp := readResp(t, dec)
	if resp.Result == nil || resp.Result.Match == nil || resp.Result.Match.Body == nil {
		t.Fatalf("expected match body, got %+v", resp)
	}
	got := resp.Result.Match.Body
	if got.Int64 == nil || *got.Int64 != want {
		t.Fatalf("clock value = %v, want %d", got, want)
	}
}

// TestServeClockWatch verifies a WATCH on a clock path emits an initial state, a
// replay-complete marker, then a state event on each tick — all stamped with the
// client's watch id — and that unwatch stops the ticker.
func TestServeClockWatch(t *testing.T) {
	start := time.Unix(1000, 0)
	freq := 15 * time.Millisecond
	clk := fixedClock("sys/clock", freq, 100, start, start.Add(30*time.Millisecond)) // tick 2
	want := int64(100 + 2*freq.Nanoseconds())

	server := New(&Spec{})
	if err := server.Clocks.register(clk); err != nil {
		t.Fatal(err)
	}

	client, peer := net.Pipe()
	defer client.Close()
	defer peer.Close()
	s := newTestClientSession(server, client)
	dec, _ := stream.NewDecoder(peer, stream.WithBrackets())

	id := "w1"
	req := &logdapi.SessionRequest{ID: &id, Watch: &logdapi.WatchRequest{Path: "sys/clock"}}
	// net.Pipe writes block until read, so serve concurrently while the test reads.
	go s.serveClockWatch(req, clk)

	// Initial state.
	init := readResp(t, dec)
	if init.Event == nil || init.Event.State == nil {
		t.Fatalf("expected initial state event, got %+v", init)
	}
	if init.ID == nil || *init.ID != id {
		t.Fatalf("initial event id = %v, want %q", init.ID, id)
	}
	if init.Event.State.Int64 == nil || *init.Event.State.Int64 != want {
		t.Fatalf("initial value = %v, want %d", init.Event.State, want)
	}

	// Replay complete.
	rc := readResp(t, dec)
	if rc.Event == nil || !rc.Event.ReplayComplete {
		t.Fatalf("expected replay-complete event, got %+v", rc)
	}

	// At least one live tick, stamped with the watch id.
	tick := readResp(t, dec)
	if tick.Event == nil || tick.Event.State == nil {
		t.Fatalf("expected tick state event, got %+v", tick)
	}
	if tick.ID == nil || *tick.ID != id {
		t.Fatalf("tick event id = %v, want %q", tick.ID, id)
	}
	if tick.Event.State.Int64 == nil || *tick.Event.State.Int64 != want {
		t.Fatalf("tick value = %v, want %d", tick.Event.State, want)
	}

	// Unwatch stops the ticker: drain any in-flight event, then confirm silence.
	s.stopClockWatch(watchKeyFor(&id, "sys/clock"))
	s.watchMu.Lock()
	n := len(s.clockWatches)
	s.watchMu.Unlock()
	if n != 0 {
		t.Fatalf("clock watch not removed after unwatch: %d remain", n)
	}
	// Drain up to one straggler already written before stop took effect, then
	// require the stream to go quiet (no further ticks).
	peer.SetReadDeadline(time.Now().Add(3 * freq))
	drainOne := make([]byte, 4096)
	_, _ = peer.Read(drainOne)
	peer.SetReadDeadline(time.Now().Add(3 * freq))
	if _, err := peer.Read(drainOne); err == nil {
		t.Fatal("ticker kept emitting after unwatch")
	}
}
