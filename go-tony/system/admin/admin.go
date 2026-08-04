// Package admin serves process introspection -- net/http/pprof plus a short
// index of what this process is and where it listens -- on a listener that is
// on by default at a known address.
//
// Two properties make this worth having next to the gops agent rather than
// instead of it.
//
// It is off the data path. The stacks worth reading are exactly the ones a
// working data path would never have produced, so nothing here touches
// storage, sessions, or any lock a request handler holds: the admin listener
// has its own accept loop and its own mux, and the addresses it reports live
// in an atomic pointer that startup writes once. A daemon whose sessions have
// all wedged can still say what its goroutines are doing.
//
// Its address is known and reported. gops binds an ephemeral port recorded in
// a per-pid file under the invoking user's config dir, which is fine on a
// laptop and unusable anywhere else -- nothing fixed to scrape, nothing
// reachable from outside a container, no access for an operator who is not the
// user that started the daemon. The admin address is a flag with a default,
// printed at startup, and echoed by the index it serves.
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// MutexProfileFraction and BlockProfileRate turn on the two profiles that
	// report nothing at all unless something enabled them first -- which, for a
	// profile you go looking for during an incident, means never. The rates are
	// low enough to leave on in a server and high enough that a minute of a
	// contended daemon yields a readable profile.
	MutexProfileFraction = 100    // sample one in 100 contention events
	BlockProfileRate     = 10_000 // one sample per 10us spent blocked
)

// Off is the address value that disables the admin listener.
const Off = "off"

// Disabled reports whether addr asks for no admin listener at all.
func Disabled(addr string) bool {
	switch strings.TrimSpace(addr) {
	case "", Off, "none":
		return true
	}
	return false
}

// Addr is a named listener address, reported by the index so an operator can
// read a daemon's ports off the daemon instead of guessing at lsof output.
type Addr struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

// Spec configures a Server.
type Spec struct {
	// Addr is the admin listen address. Disabled values (see Disabled) mean no
	// listener; Start then succeeds having done nothing.
	Addr string

	// Name identifies the process in the index, e.g. "o sys up".
	Name string

	// Log receives the startup line and any serve error. Defaults to discarding.
	Log *slog.Logger
}

// Server is the admin listener.
type Server struct {
	spec    Spec
	started time.Time

	// addrs is written by SetAddrs at startup and read by request handlers.
	// An atomic pointer rather than a mutex: a handler that reports addresses
	// must not be able to block behind anything, however briefly.
	addrs atomic.Pointer[[]Addr]

	ln   net.Listener
	http *http.Server
}

// New creates an admin server. It does not listen until Start.
func New(spec *Spec) *Server {
	s := &Server{spec: *spec}
	if s.spec.Log == nil {
		s.spec.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return s
}

// SetAddrs records the addresses the index reports. Call it once the data
// listeners have resolved, since a port asked for as :0 is not known until
// then. Safe to call while serving.
func (s *Server) SetAddrs(addrs ...Addr) {
	s.addrs.Store(&addrs)
}

// Start binds the admin address and serves in the background. A bind failure
// is an error: an introspection channel that is quietly absent is the thing
// this package exists to fix, so a taken port has to be moved or disabled on
// purpose.
func (s *Server) Start() error {
	if Disabled(s.spec.Addr) {
		s.spec.Log.Warn("admin listener disabled", "addr", s.spec.Addr)
		return nil
	}

	ln, err := net.Listen("tcp", s.spec.Addr)
	if err != nil {
		return fmt.Errorf("admin listener: %w (pass -admin-addr <addr> to move it, or -admin-addr %s to disable it)", err, Off)
	}

	// Both profiles are process-global and both are empty until set. Turning
	// them on here, rather than behind a flag, is the point: the mutex profile
	// you need is the one that was already sampling when the contention
	// happened.
	if runtime.SetMutexProfileFraction(-1) == 0 {
		runtime.SetMutexProfileFraction(MutexProfileFraction)
	}
	runtime.SetBlockProfileRate(BlockProfileRate)

	s.started = time.Now()
	s.ln = ln
	s.http = &http.Server{
		Handler:           s.mux(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		// No WriteTimeout: /debug/pprof/profile?seconds=120 is a legitimate
		// request and a write deadline would cut it off mid-profile.
	}

	go func() {
		err := s.http.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.spec.Log.Error("admin listener stopped", "error", err)
		}
	}()

	s.spec.Log.Info("admin listener started", "addr", s.Addr(), "pprof", s.URL()+"/debug/pprof/")
	return nil
}

// Addr returns the bound admin address, or "" if the listener is disabled or
// not started.
func (s *Server) Addr() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// URL returns a base URL a person can paste. A listener bound to every
// interface reports its address as [::]:9223, which is not something a browser
// or curl will take.
func (s *Server) URL() string {
	addr := s.Addr()
	if addr == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "::" || host == "0.0.0.0" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// Close stops serving.
func (s *Server) Close() error {
	if s.http == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.http.Shutdown(ctx)
}

// mux builds the admin routes on a private mux. net/http/pprof registers its
// handlers on http.DefaultServeMux in an init function; routing them here
// keeps them off whatever else in the process might serve that mux.
func (s *Server) mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.serveIndex)
	mux.HandleFunc("/debug/admin/info", s.serveInfo)
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}

// Info is what the admin listener knows about its process.
type Info struct {
	Name         string    `json:"name"`
	PID          int       `json:"pid"`
	Executable   string    `json:"executable"`
	GoVersion    string    `json:"goVersion"`
	Platform     string    `json:"platform"`
	GOMAXPROCS   int       `json:"gomaxprocs"`
	NumCPU       int       `json:"numCPU"`
	NumGoroutine int       `json:"numGoroutine"`
	StartedAt    time.Time `json:"startedAt"`
	Uptime       string    `json:"uptime"`
	Addrs        []Addr    `json:"addrs"`
}

// Info snapshots the process state the index reports.
func (s *Server) Info() Info {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	addrs := []Addr{{Name: "admin", Addr: s.Addr()}}
	if p := s.addrs.Load(); p != nil {
		addrs = append(addrs, *p...)
	}
	return Info{
		Name:         s.spec.Name,
		PID:          os.Getpid(),
		Executable:   exe,
		GoVersion:    runtime.Version(),
		Platform:     runtime.GOOS + "/" + runtime.GOARCH,
		GOMAXPROCS:   runtime.GOMAXPROCS(0),
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
		StartedAt:    s.started,
		Uptime:       time.Since(s.started).Round(time.Second).String(),
		Addrs:        addrs,
	}
}

func (s *Server) serveInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(s.Info())
}

// serveIndex answers "what is this process and what can I ask it", which is
// the question an operator has when they have just found an unexplained
// listener.
func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	info := s.Info()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	fmt.Fprintf(w, "%s\n", info.Name)
	fmt.Fprintf(w, "  pid         %d\n", info.PID)
	fmt.Fprintf(w, "  exe         %s\n", info.Executable)
	fmt.Fprintf(w, "  go          %s %s\n", info.GoVersion, info.Platform)
	fmt.Fprintf(w, "  procs       %d of %d cpu\n", info.GOMAXPROCS, info.NumCPU)
	fmt.Fprintf(w, "  goroutines  %d\n", info.NumGoroutine)
	fmt.Fprintf(w, "  uptime      %s\n", info.Uptime)

	fmt.Fprintf(w, "\nlistening\n")
	width := 0
	for _, a := range info.Addrs {
		if len(a.Name) > width {
			width = len(a.Name)
		}
	}
	for _, a := range info.Addrs {
		fmt.Fprintf(w, "  %-*s  %s\n", width, a.Name, a.Addr)
	}

	fmt.Fprintf(w, "\nprofiles\n")
	for _, l := range [][2]string{
		{"/debug/pprof/", "index of every profile"},
		{"/debug/pprof/goroutine?debug=2", "full goroutine stacks, what gops stack gives you"},
		{"/debug/pprof/profile?seconds=30", "cpu"},
		{"/debug/pprof/heap", "heap"},
		{"/debug/pprof/mutex", "mutex contention"},
		{"/debug/pprof/block", "blocking"},
		{"/debug/admin/info", "this page as json"},
	} {
		fmt.Fprintf(w, "  %-32s %s\n", l[0], l[1])
	}
}
