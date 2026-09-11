package server

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// A watch steps its value by a !nullify write and says so. It said nothing: stepping
// folded the operation into the value it held and rewrote it, so the next value was the
// same object as the last and the commit counted as no change (fk1vg9sxh12ksyxxmdn0).
func TestWatchSeesANullifyWrite(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, src := range []string{`{spec: {a: 1}, k: 1}`, `{spec: !nullify null}`, `{k: 2}`} {
		data, err := parse.Parse([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		tx, err := store.NewTx(1, nil)
		if err != nil {
			t.Fatal(err)
		}
		p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: data}})
		if err != nil {
			t.Fatal(err)
		}
		if res := p.Commit(); !res.Committed {
			t.Fatalf("commit %s: %v", src, res.Error)
		}
	}

	for _, path := range []string{"", "spec"} {
		t.Run("at "+path, func(t *testing.T) {
			events := narrowRequestEvents(t, store, `{id: "w", watch: {path: "`+path+`", fromCommit: 1}}`)
			var commits []int64
			for _, ev := range events {
				if ev.Patch != nil {
					commits = append(commits, ev.Commit)
				}
			}
			saw := false
			for _, c := range commits {
				saw = saw || c == 2
			}
			if !saw {
				t.Errorf("the watch delivered deltas for commits %v; the !nullify write is commit 2", commits)
			}
		})
	}
}
