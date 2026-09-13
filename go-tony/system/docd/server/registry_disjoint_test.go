package server

import (
	"errors"
	"testing"
)

// Mounts are disjoint: a mount above or below a registered mount is refused, live
// or tombstoned; a sibling, and a remount over a tombstone at the same path, are not.
func TestRegister_RefusesAMountAboveOrBelowAnother(t *testing.T) {
	r := NewMountRegistry()
	live := &MountSession{}
	must := func(path string) {
		t.Helper()
		if err := r.Register(&MountEntry{Path: path, Controller: "c", Session: live}); err != nil {
			t.Fatalf("mount %q: %v", path, err)
		}
	}
	refused := func(path string) {
		t.Helper()
		err := r.Register(&MountEntry{Path: path, Controller: "c", Session: live})
		if !errors.Is(err, ErrPathOverlapsMount) {
			t.Fatalf("mount %q: got %v, want ErrPathOverlapsMount", path, err)
		}
		if r.Lookup(path) != nil {
			t.Fatalf("mount %q was registered despite the refusal", path)
		}
	}

	must("users")
	refused("users.alice.inbox") // under
	refused("users.alice")       // under, one level
	must("orders")               // sibling
	must("usersx")               // a field that merely shares a prefix of letters
	refused("")                  // the root is above everything

	// Above: a mount at users is gone, and a mount below it is live.
	r2 := NewMountRegistry()
	if err := r2.Register(&MountEntry{Path: "a.b.c", Controller: "c", Session: live}); err != nil {
		t.Fatal(err)
	}
	if err := r2.Register(&MountEntry{Path: "a.b", Controller: "c", Session: live}); !errors.Is(err, ErrPathOverlapsMount) {
		t.Fatalf("mount above: got %v", err)
	}
	if err := r2.Register(&MountEntry{Path: "a", Controller: "c", Session: live}); !errors.Is(err, ErrPathOverlapsMount) {
		t.Fatalf("mount two above: got %v", err)
	}

	// A tombstone is a claim awaiting its remount: still an overlap, and the remount
	// at the same path still lands.
	r.TombstoneBySession("users", live)
	if r.Lookup("users").Live() {
		t.Fatal("users should be tombstoned")
	}
	refused("users.alice")
	must("users")
}
