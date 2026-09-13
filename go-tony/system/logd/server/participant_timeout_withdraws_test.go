package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A participant answered timeout has withdrawn: its patch is not part of the
// transaction, which goes on waiting for that participant. It was answered timeout
// and left joined, so the next participant to arrive completed the transaction and
// committed a write its author had been told failed, under a commit it never saw --
// and a retry applied it twice (4ynqp7wqh12krg32msn0 item 3).
func TestParticipantAnsweredTimeoutHasWithdrawn(t *testing.T) {
	c := dialTx(t, startTxServer(t))
	txID := c.newTx(`{participants: 2, timeout: "5s"}`)

	// p1 gives up first.
	c.send(fmt.Sprintf(`{id: p1, patch: {txId: %d, path: a, data: {n: 1}, timeout: "100ms"}}`, txID))
	resp := c.recv(2 * time.Second)
	if resp.Error == nil || resp.Error.Code != api.ErrCodeTimeout {
		t.Fatalf("p1 answered %+v, want timeout", resp)
	}

	// p2 alone does not complete a transaction of two: p1 is gone, not joined.
	c.send(fmt.Sprintf(`{id: p2, patch: {txId: %d, path: b, data: {n: 2}, timeout: "200ms"}}`, txID))
	resp = c.recv(2 * time.Second)
	if resp.Error == nil || resp.Error.Code != api.ErrCodeTimeout {
		t.Fatalf("p2 alone answered %+v, want timeout: p1's withdrawn patch completed the transaction", resp)
	}

	// Both retry, in flight together: one commit, each write once.
	c.send(fmt.Sprintf(`{id: p1b, patch: {txId: %d, path: a, data: {n: 1}}}`, txID))
	c.send(fmt.Sprintf(`{id: p2b, patch: {txId: %d, path: b, data: {n: 2}}}`, txID))
	var commits []int64
	for i := 0; i < 2; i++ {
		resp = c.recv(3 * time.Second)
		if resp.Error != nil || resp.Result == nil || resp.Result.Patch == nil {
			t.Fatalf("retry answered %+v, want a commit", resp)
		}
		commits = append(commits, resp.Result.Patch.Commit)
	}
	if commits[0] != commits[1] {
		t.Errorf("the retries report commits %v, want the same one", commits)
	}

	c.send(`{id: m, match: {path: ""}}`)
	resp = c.recv(2 * time.Second)
	if resp.Error != nil || resp.Result == nil || resp.Result.Match == nil {
		t.Fatalf("match: %+v", resp)
	}
	body := resp.Result.Match.Body
	if n := ir.Get(ir.Get(body, "a"), "n"); n == nil || n.Int64 == nil || *n.Int64 != 1 {
		t.Errorf("a.n = %v, want 1 (p1's retry, once)", n)
	}
	if n := ir.Get(ir.Get(body, "b"), "n"); n == nil || n.Int64 == nil || *n.Int64 != 2 {
		t.Errorf("b.n = %v, want 2", n)
	}
	if resp.Result.Match.Commit != commits[0] {
		t.Errorf("head is commit %d, want %d: the one commit the retries made", resp.Result.Match.Commit, commits[0])
	}
}
