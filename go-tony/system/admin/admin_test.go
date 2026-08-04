package admin

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"testing"
)

func get(t *testing.T, base, path string) (int, string) {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: reading body: %v", path, err)
	}
	return resp.StatusCode, string(body)
}

func TestServerReportsItselfAndProfiles(t *testing.T) {
	s := New(&Spec{Addr: "127.0.0.1:0", Name: "o test serve"})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetAddrs(Addr{Name: "logd", Addr: "127.0.0.1:9123"})
	base := s.URL()

	// The index says who this is and every address it listens on, which is the
	// question an operator has after finding an unexplained listener.
	code, body := get(t, base, "/")
	if code != http.StatusOK {
		t.Fatalf("index: got %d, want 200", code)
	}
	for _, want := range []string{"o test serve", s.Addr(), "127.0.0.1:9123", "/debug/pprof/"} {
		if !strings.Contains(body, want) {
			t.Errorf("index does not mention %q:\n%s", want, body)
		}
	}

	// Goroutine stacks, the thing gops stack gives you.
	code, body = get(t, base, "/debug/pprof/goroutine?debug=2")
	if code != http.StatusOK || !strings.Contains(body, "goroutine ") {
		t.Errorf("goroutine profile: got %d, %.80q", code, body)
	}

	// The profiles gops does not expose are sampling, not empty by default.
	if runtime.SetMutexProfileFraction(-1) == 0 {
		t.Error("mutex profiling still off after Start")
	}
	for _, p := range []string{"/debug/pprof/mutex", "/debug/pprof/block", "/debug/pprof/heap"} {
		if code, _ := get(t, base, p); code != http.StatusOK {
			t.Errorf("%s: got %d, want 200", p, code)
		}
	}

	code, body = get(t, base, "/debug/admin/info")
	if code != http.StatusOK {
		t.Fatalf("info: got %d, want 200", code)
	}
	var info Info
	if err := json.Unmarshal([]byte(body), &info); err != nil {
		t.Fatalf("info is not json: %v\n%s", err, body)
	}
	if info.Name != "o test serve" || len(info.Addrs) != 2 {
		t.Errorf("info: got %+v", info)
	}

	// An unknown path is a 404, not the index.
	if code, _ := get(t, base, "/nope"); code != http.StatusNotFound {
		t.Errorf("/nope: got %d, want 404", code)
	}
}

func TestDisabled(t *testing.T) {
	for _, addr := range []string{"", "off", "none", "  off  "} {
		if !Disabled(addr) {
			t.Errorf("Disabled(%q) = false", addr)
		}
	}
	if Disabled("localhost:9223") {
		t.Error(`Disabled("localhost:9223") = true`)
	}

	s := New(&Spec{Addr: Off, Name: "o test serve"})
	if err := s.Start(); err != nil {
		t.Fatalf("disabled admin listener should start cleanly: %v", err)
	}
	defer s.Close()
	if got := s.Addr(); got != "" {
		t.Errorf("disabled listener bound %s", got)
	}
}

func TestTakenPortIsAnError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	s := New(&Spec{Addr: ln.Addr().String(), Name: "o test serve"})
	if err := s.Start(); err == nil {
		s.Close()
		t.Fatal("a taken admin port must fail startup, not leave the daemon silently uninspectable")
	} else if !strings.Contains(err.Error(), "-admin-addr") {
		t.Errorf("error should name the way out: %v", err)
	}
}
