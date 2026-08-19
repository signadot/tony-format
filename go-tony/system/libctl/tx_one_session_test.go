package libctl

import (
	"context"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// A client may commit several paths atomically over ONE session. It could not: a patch
// joining a transaction waits for the other participants, and it waited on the request
// loop -- so the participants pipelined behind it were never read, the transaction timed
// out, and every participant failed:
//
//	{error: {code: storage_error message: "not all participants joined within 1s"} id: p1}
//	{error: {code: tx_not_found ...} id: p2}
//
// The documented shape of a transaction was therefore unrunnable as written, which is how
// this was found.
func TestTransactionOverOneSession(t *testing.T) {
	srv := startLogd(t)
	ctx := context.Background()
	s := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "tx-one"})
	defer s.Close()
	if err := s.Connect(ctx); err != nil {
		t.Fatalf("connect: %s", err)
	}

	txID, err := s.NewTx(ctx, 2)
	if err != nil {
		t.Fatalf("newtx: %s", err)
	}

	type res struct {
		commit int64
		err    error
	}
	done := make(chan res, 2)
	for i, path := range []string{"verse.a", "verse.b"} {
		go func(path string, n int64) {
			r, err := s.PatchTx(ctx, path, ir.FromMap(map[string]*ir.Node{"n": ir.FromInt(n)}), txID)
			if err != nil {
				done <- res{err: err}
				return
			}
			done <- res{commit: r.Commit}
		}(path, int64(i+1))
	}

	var commits []int64
	for i := 0; i < 2; i++ {
		r := <-done
		if r.err != nil {
			t.Fatalf("participant %d: %s", i, r.err)
		}
		commits = append(commits, r.commit)
	}
	if commits[0] != commits[1] {
		t.Errorf("participants report commits %d and %d; a transaction is one commit", commits[0], commits[1])
	}

	// And both writes are there.
	for _, path := range []string{"verse.a", "verse.b"} {
		got, err := s.Match(ctx, path)
		if err != nil {
			t.Fatalf("read %s: %s", path, err)
		}
		if got == nil {
			t.Errorf("%s is empty after the transaction committed", path)
		}
	}
}
