package storage

import (
	"errors"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// A commit records who wrote it (cn1n32yph12ks5wrmhn0): the author its participants
// named goes on the entry beside the timestamp, on the notification, and on a replay of
// the entry, so a watcher and a `since` reader see it without a second lookup.
func TestCommitRecordsItsAuthor(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	var notified []*CommitNotification
	s.SetCommitNotifier(func(n *CommitNotification) { notified = append(notified, n) })

	write := func(path, author string, data string) int64 {
		t.Helper()
		txn, err := s.NewTx(1, nil)
		if err != nil {
			t.Fatalf("NewTx: %v", err)
		}
		p, err := txn.NewPatcher(&api.Patch{Author: author, PathData: api.PathData{Path: path, Data: mustParseTony(t, data)}})
		if err != nil {
			t.Fatalf("NewPatcher: %v", err)
		}
		res := p.Commit()
		if res.Error != nil {
			t.Fatalf("Commit: %v", res.Error)
		}
		return res.Commit
	}
	byAlice := write("a", "alice", `{n: 1}`)
	byNone := write("a", "", `{n: 2}`)
	s.tick.waitDrained()

	if len(notified) != 2 {
		t.Fatalf("notified %d commits, want 2", len(notified))
	}
	if notified[0].Author != "alice" || notified[1].Author != "" {
		t.Errorf("notifications say authors %q and %q, want alice and none", notified[0].Author, notified[1].Author)
	}

	cur, err := s.Deltas(byAlice, byNone, nil, "a")
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
	if len(replayed) != 2 || replayed[0] != "alice" || replayed[1] != "" {
		t.Errorf("replay says authors %q, want [alice \"\"]", replayed)
	}
}

// A commit has one author. A participant naming another than the transaction's, none
// against some included, is refused at the join, and the transaction stands for the
// participant that names the right one.
func TestTransactionHasOneAuthor(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	txn, err := s.NewTx(2, nil)
	if err != nil {
		t.Fatalf("NewTx: %v", err)
	}
	first, err := txn.NewPatcher(&api.Patch{Author: "alice", PathData: api.PathData{Path: "a", Data: mustParseTony(t, `1`)}})
	if err != nil {
		t.Fatalf("first participant: %v", err)
	}
	for _, other := range []string{"bob", ""} {
		_, err := txn.NewPatcher(&api.Patch{Author: other, PathData: api.PathData{Path: "b", Data: mustParseTony(t, `2`)}})
		var mismatch *tx.AuthorMismatchError
		if !errors.As(err, &mismatch) {
			t.Fatalf("a participant by %q joined alice's transaction: err = %v", other, err)
		}
		if mismatch.Transactions != "alice" || mismatch.Participant != other {
			t.Errorf("mismatch = %+v, want alice vs %q", mismatch, other)
		}
	}
	second, err := txn.NewPatcher(&api.Patch{Author: "alice", PathData: api.PathData{Path: "b", Data: mustParseTony(t, `2`)}})
	if err != nil {
		t.Fatalf("the transaction did not stand for a participant by alice: %v", err)
	}
	go first.Commit()
	res := second.Commit()
	if res.Error != nil {
		t.Fatalf("Commit: %v", res.Error)
	}

	cur, err := s.Deltas(res.Commit, res.Commit, nil, "")
	if err != nil {
		t.Fatalf("Deltas: %v", err)
	}
	defer cur.Close()
	n, err := cur.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if n.Author != "alice" {
		t.Errorf("the commit is recorded as written by %q, want alice", n.Author)
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
