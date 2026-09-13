package server

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// An absolute fromCommit past the head names no commit, and a watch refuses it as a
// match does. It was accepted: the state went out stamped with the fictitious commit,
// and a resume point past the head came back with any later end
// (p478tacqh12krg32msn0 item 11).
func TestWatchFromACommitPastTheHeadIsRefused(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()
	narrowWrite(t, store, "", `{a: 1}`)
	narrowWrite(t, store, "", `{a: 2}`)
	head, _ := store.GetCurrentCommit()

	hub := NewWatchHub()
	byID := runRequests(t, store, hub,
		`{id: "past", watch: {path: "a", fromCommit: 100}}`,
		`{id: "at", watch: {path: "a", fromCommit: `+itoa(int(head))+`}}`,
		`{id: "rel", watch: {path: "a", fromCommit: -100}}`,
	)
	if r := byID["past"]; r == nil || r.Error == nil || r.Error.Code != api.ErrCodeCommitNotFound {
		t.Errorf("fromCommit 100 at head %d answered %v, want %s", head, r, api.ErrCodeCommitNotFound)
	}
	for _, id := range []string{"at", "rel"} {
		if r := byID[id]; r == nil || r.Error != nil {
			t.Errorf("%s: %v, want the watch confirmed", id, r)
		}
	}
}
