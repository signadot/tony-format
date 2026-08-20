package server

import (
	"io"
	"net"
	"testing"
	"time"

	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A route is removed when its response arrives. A caller which gives up waiting -- every
// one has a timeout -- used to leave the entry behind for the life of the mount session,
// holding its channel and its path: one per timed-out read or transaction participant,
// forever, on exactly the controller which is already unwell.
func TestACollectedRouteIsFreedWhenTheCallerGivesUp(t *testing.T) {
	// A controller which reads its requests and answers none of them.
	ours, theirs := net.Pipe()
	defer ours.Close()
	defer theirs.Close()
	go io.Copy(io.Discard, theirs)

	s := &MountSession{
		controllerID: "silent",
		conn:         ours,
		routes:       map[string]*routeEntry{},
	}

	ch, done := s.RouteCollect(&logdapi.SessionRequest{
		Match: &logdapi.MatchRequest{PathData: logdapi.PathData{Path: "a.b"}},
	})

	s.routeMu.Lock()
	registered := len(s.routes)
	s.routeMu.Unlock()
	if registered != 1 {
		t.Fatalf("%d routes registered, want 1", registered)
	}

	select {
	case <-ch:
		t.Fatal("the silent controller answered")
	case <-time.After(100 * time.Millisecond):
		// what every caller does: give up
	}
	done()

	s.routeMu.Lock()
	n := len(s.routes)
	s.routeMu.Unlock()
	if n != 0 {
		t.Errorf("%d routes left after the caller gave up; they are never collected again", n)
	}
}
