package server

import (
	"strconv"
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// A watched path that holds nothing, and holds nothing after an operator above it
// replaced the container, has not changed. The watch reads the value at the path to
// find that out, and a read of nothing against nothing is no delta at all: the process
// crashed here on the diff of two absences before it asked whether anything changed
// (05d8w3cjh12kswb1msn0, item 1).
func TestWatchAbsentUnderReplacedContainerStaysAbsent(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()

	narrowWrite(t, store, "", "{a: {b: 1}}")
	before, _ := store.GetCurrentCommit()
	narrowWrite(t, store, "", "{a: !insert {x: 1}}") // b leaves with its container
	goneAt, _ := store.GetCurrentCommit()
	narrowWrite(t, store, "", "{a: !insert {y: 1}}") // the container is replaced again; b is still nothing
	narrowWrite(t, store, "", "{a: !insert {b: 2}}") // and b arrives again
	backAt, _ := store.GetCurrentCommit()

	events := narrowRequestEvents(t, store,
		`{id: "w", watch: {path: "a.b", fromCommit: `+strconv.FormatInt(before, 10)+`, waitIfAbsent: true}}`)

	var patched []int64
	for _, ev := range events {
		if ev.Patch != nil {
			patched = append(patched, ev.Commit)
		}
	}
	want := []int64{goneAt, backAt}
	if len(patched) != len(want) {
		t.Fatalf("patch events at commits %v, want %v (deleted at %d, back at %d)",
			patched, want, goneAt, backAt)
	}
	for i := range want {
		if patched[i] != want[i] {
			t.Errorf("event %d at commit %d, want %d", i, patched[i], want[i])
		}
	}
}
