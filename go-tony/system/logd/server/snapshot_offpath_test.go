package server

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// The write which trips the snapshot threshold used to pay for the snapshot: the
// trigger ran inside handlePatch, before that patch's response went out, so one
// unremarkable write took a full snapshot of the store -- plus CheckHead, which reads
// the whole document -- while every write behind it on the session waited. Staging saw
// it as a client deadline on a write nobody was doing anything unusual with, landing on
// a different write each time, which is what made it look random
// (dvgz9308h12ks4xmgdn0).
//
// A snapshot does not need the writer, and double-buffered logs are what make that
// safe: the active log is switched first, so commits during the snapshot land in the
// new log.
func TestSnapshotDoesNotRideOnTheWriteThatTripsIt(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()

	// A store big enough that a snapshot of it is not free.
	blob := strings.Repeat("x", 400)
	srv := New(&Spec{Storage: store, Config: &Config{Snapshot: &SnapshotConfig{MaxCommits: 1 << 30}}})
	for i := 0; i < 1500; i++ {
		id := "e" + strconv.Itoa(i)
		doPatch(t, store, srv, "verse.entities."+id, mustParse(`{id: "`+id+`", blob: "`+blob+`"}`))
	}

	// What a snapshot of it costs, and what a write costs, measured here rather than
	// assumed -- the bound below comes from them.
	start := time.Now()
	if err := store.SwitchDLog(); err != nil {
		t.Fatalf("snapshot: %s", err)
	}
	snapshotTook := time.Since(start)

	start = time.Now()
	doPatch(t, store, srv, "verse.meta", mustParse(`{rev: 1}`))
	writeTook := time.Since(start)
	t.Logf("alone: snapshot %s, write %s", snapshotTook.Round(time.Millisecond), writeTook.Round(time.Millisecond))
	if snapshotTook < 10*writeTook {
		t.Skipf("a snapshot (%s) is not slow enough against a write (%s) here to tell the two cases apart",
			snapshotTook, writeTook)
	}

	// Now arm the threshold so the very next write trips it.
	srv.Spec.Config.Snapshot.MaxCommits = 1
	srv.commitsSinceSnapshot.Store(0)

	start = time.Now()
	doPatch(t, store, srv, "verse.meta", mustParse(`{rev: 2}`))
	tripped := time.Since(start)
	t.Logf("the write that trips the threshold: %s (a snapshot alone takes %s)",
		tripped.Round(time.Millisecond), snapshotTook.Round(time.Millisecond))

	// The yardstick is what a write costs, not what a snapshot costs: the triggering
	// write must cost about what its neighbours cost. Twenty times that, plus 5ms of
	// floor for a machine under load, is far above the noise and far below the
	// hundred-odd milliseconds of snapshot the old code charged it.
	if budget := 20*writeTook + 5*time.Millisecond; tripped > budget {
		t.Errorf("the triggering write took %s against a budget of %s (a plain write is %s, a snapshot %s): it paid for the snapshot",
			tripped.Round(time.Millisecond), budget.Round(time.Millisecond),
			writeTook.Round(time.Millisecond), snapshotTook.Round(time.Millisecond))
	}

	// And it did happen: the counter comes back down once the snapshot lands.
	srv.awaitSnapshots()
	if n := srv.commitsSinceSnapshot.Load(); n != 0 {
		t.Errorf("after the snapshot, commitsSinceSnapshot is %d, want 0", n)
	}
}

// Writes keep landing while a snapshot runs, and they are commits since THIS
// snapshot: zeroing the counter afterwards would forget them and delay the next one.
func TestCommitsDuringASnapshotStillCount(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()

	srv := New(&Spec{Storage: store, Config: &Config{Snapshot: &SnapshotConfig{MaxCommits: 2}}})
	for i := 0; i < 2; i++ {
		doPatch(t, store, srv, "verse.entities.e"+strconv.Itoa(i), mustParse(`{id: "e"}`))
	}
	// The second commit tripped it, counting two. Disarm the policy so what follows
	// measures the bookkeeping of THAT snapshot and not another one behind it.
	srv.Spec.Config.Snapshot.MaxCommits = 1 << 30

	for i := 0; i < 3; i++ {
		doPatch(t, store, srv, "verse.late.e"+strconv.Itoa(i), mustParse(`{id: "late"}`))
	}
	srv.awaitSnapshots()

	// Exactly the two it counted come off; the three after it stand.
	if n := srv.commitsSinceSnapshot.Load(); n != 3 {
		t.Errorf("commitsSinceSnapshot is %d after 3 writes past the trigger, want 3: they were swallowed", n)
	}
}
