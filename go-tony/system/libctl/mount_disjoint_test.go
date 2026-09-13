package libctl

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/system/docd/api"
	"github.com/signadot/tony-format/go-tony/system/docd/server"
)

// Mounts are disjoint (docs/docd/mounts.md): a controller that mounts under, or
// above, a mounted path is refused with path_overlaps_mount, and the registry is
// unchanged by the attempt.
func TestMount_RefusedAboveOrBelowAMount(t *testing.T) {
	srv := server.New(&server.Spec{})
	if err := srv.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start docd: %v", err)
	}
	defer srv.StopTCP()

	outer, err := Mount(&MountConfig{DocdAddr: srv.TCPAddr(), Controller: "outer", Path: "users"})
	if err != nil {
		t.Fatalf("mount users: %v", err)
	}
	defer outer.Close()

	for _, path := range []string{"users.alice.inbox", "users.alice"} {
		inner, err := Mount(&MountConfig{DocdAddr: srv.TCPAddr(), Controller: "inner", Path: path})
		if err == nil {
			inner.Close()
			t.Fatalf("mount %q under users succeeded", path)
		}
		if !strings.Contains(err.Error(), api.ErrCodePathOverlapsMount) {
			t.Fatalf("mount %q: got %v, want %s", path, err, api.ErrCodePathOverlapsMount)
		}
		if srv.Mounts.Lookup(path) != nil {
			t.Fatalf("mount %q registered despite the refusal", path)
		}
	}

	// Below first, then above: refused the same way.
	leaf, err := Mount(&MountConfig{DocdAddr: srv.TCPAddr(), Controller: "leaf", Path: "org.team.inbox"})
	if err != nil {
		t.Fatalf("mount org.team.inbox: %v", err)
	}
	defer leaf.Close()
	above, err := Mount(&MountConfig{DocdAddr: srv.TCPAddr(), Controller: "above", Path: "org"})
	if err == nil {
		above.Close()
		t.Fatal("mount org above org.team.inbox succeeded")
	}
	if !strings.Contains(err.Error(), api.ErrCodePathOverlapsMount) {
		t.Fatalf("mount org: got %v, want %s", err, api.ErrCodePathOverlapsMount)
	}

	// A sibling is not an overlap.
	sib, err := Mount(&MountConfig{DocdAddr: srv.TCPAddr(), Controller: "sib", Path: "org.other"})
	if err != nil {
		t.Fatalf("mount org.other beside org.team.inbox: %v", err)
	}
	sib.Close()
}
