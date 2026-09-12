package storage

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A commit records who wrote it (cn1n32yph12ks5wrmhn0): the transaction's author goes
// on the entry beside the timestamp, on the notification, and on a replay of the entry,
// so a watcher and a `since` reader see it without a second lookup. The author is the
// transaction's, fixed when it is created; a participant has none of its own, which is
// what makes a transaction one principal's by construction.
func TestCommitRecordsItsAuthor(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	var notified []*CommitNotification
	s.SetCommitNotifier(func(n *CommitNotification) { notified = append(notified, n) })

	commit := func(author string, patches ...api.PathData) int64 {
		t.Helper()
		txn, err := s.NewTxWithTimeout(len(patches), nil, 0, author)
		if err != nil {
			t.Fatalf("NewTxWithTimeout: %v", err)
		}
		results := make(chan int64, len(patches))
		for _, pd := range patches {
			p, err := txn.NewPatcher(&api.Patch{PathData: pd})
			if err != nil {
				t.Fatalf("NewPatcher: %v", err)
			}
			go func() {
				res := p.Commit()
				if res.Error != nil {
					t.Errorf("Commit: %v", res.Error)
				}
				results <- res.Commit
			}()
		}
		var c int64
		for range patches {
			c = <-results
		}
		return c
	}
	first := commit("alice", api.PathData{Path: "a", Data: mustParseTony(t, `{n: 1}`)})
	commit("bob", api.PathData{Path: "a", Data: mustParseTony(t, `{n: 2}`)}, api.PathData{Path: "b", Data: mustParseTony(t, `{n: 2}`)})
	last := commit("", api.PathData{Path: "a", Data: mustParseTony(t, `{n: 3}`)})
	s.tick.waitDrained()

	want := []string{"alice", "bob", ""}
	if len(notified) != len(want) {
		t.Fatalf("notified %d commits, want %d", len(notified), len(want))
	}
	for i, n := range notified {
		if n.Author != want[i] {
			t.Errorf("notification %d says author %q, want %q", i, n.Author, want[i])
		}
	}

	cur, err := s.Deltas(first, last, nil, "")
	if err != nil {
		t.Fatalf("Deltas: %v", err)
	}
	defer cur.Close()
	var replayed []string
	for {
		n, err := cur.Next()
		if err != nil {
			break
		}
		replayed = append(replayed, n.Author)
	}
	if len(replayed) != len(want) {
		t.Fatalf("replayed %d commits, want %d", len(replayed), len(want))
	}
	for i, a := range replayed {
		if a != want[i] {
			t.Errorf("replayed commit %d says author %q, want %q", i, a, want[i])
		}
	}
}

func mustParseTony(t *testing.T, src string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return n
}
