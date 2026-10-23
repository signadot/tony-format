package server

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A scratch operation goes to logd whatever its path spells: a scratch path is into
// the store's scratch document, not this one, so a mount can never hold scratch
// (r482sz5rh12ksj0cvdn0). A match at the same spelling goes to the mount.
func TestScratch_RoutesToLogd(t *testing.T) {
	cs := &ClientSession{server: &Server{Mounts: regWith("verse")}}
	for _, req := range []*logdapi.SessionRequest{
		{Scratch: &logdapi.ScratchRequest{Read: &logdapi.ScratchReadRequest{Path: "verse.iods.x.out"}}},
		{Scratch: &logdapi.ScratchRequest{Write: &logdapi.ScratchWriteRequest{
			PathData: logdapi.PathData{Path: "verse.iods.x.out", Data: ir.FromInt(1)}}}},
	} {
		if p := requestPath(req); p != "" {
			t.Fatalf("requestPath of a scratch op = %q, want \"\"", p)
		}
		if dest, entry := cs.routeFor(req); dest != destLogd || entry != nil {
			t.Fatalf("a scratch op under a mounted spelling routed to %v (%v), want logd", dest, entry)
		}
	}
	match := &logdapi.SessionRequest{Match: &logdapi.MatchRequest{PathData: logdapi.PathData{Path: "verse.iods.x.out"}}}
	if dest, _ := cs.routeFor(match); dest != destController {
		t.Fatalf("a match at the mounted spelling routed to %v, want the controller", dest)
	}
}
