package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

// TestBlocks_ReciprocalWriteFailsLoudlyAndRerunRepairs: blocks is written to both
// issues, and the second write can fail after the first has landed. That failure was
// dropped, so blocks reported success with blocked_by missing; and a rerun asked only
// the first issue whether the relationship was there, answered "already has this
// relationship", and could not write the missing half (addsgv1yh12kszdxmdn0).
func TestBlocks_ReciprocalWriteFailsLoudlyAndRerunRepairs(t *testing.T) {
	store := testRepo(t)
	a, err := store.Create("A", "# A\n")
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := store.Create("B", "# B\n")
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	cfg := &relateConfig{store: store, relationType: "blocks"}

	// A held lock on B's ref fails B's update-ref, and nothing else.
	lock := filepath.Join(".git", b.Ref+".lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatalf("lock B's ref: %v", err)
	}
	if err := cfg.run(pushCC(), []string{a.ID, b.ID}); err == nil {
		t.Error("blocks reported success though the blocked_by write to B failed")
	}
	if err := os.Remove(lock); err != nil {
		t.Fatalf("unlock B's ref: %v", err)
	}

	out := &strings.Builder{}
	cc := &cli.Context{Out: nopWriteCloser{out}, Err: nopWriteCloser{&strings.Builder{}}}
	if err := cfg.run(cc, []string{a.ID, b.ID}); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if strings.Contains(out.String(), "already has") {
		t.Errorf("rerun answered %q with B's blocked_by still missing", strings.TrimSpace(out.String()))
	}
	got := func(id string) *issuelib.Issue {
		t.Helper()
		ref, err := store.FindRef(id)
		if err != nil {
			t.Fatalf("find %s: %v", id, err)
		}
		issue, _, err := store.GetByRef(ref)
		if err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return issue
	}
	if ga := got(a.ID); !issuelib.Contains(ga.Blocks, b.ID) || len(ga.Blocks) != 1 {
		t.Errorf("A.blocks = %v, want exactly [%s]", ga.Blocks, b.ID)
	}
	if gb := got(b.ID); !issuelib.Contains(gb.BlockedBy, a.ID) || len(gb.BlockedBy) != 1 {
		t.Errorf("B.blocked_by = %v, want exactly [%s]", gb.BlockedBy, a.ID)
	}

	// With both halves written there is nothing left to do.
	out.Reset()
	if err := cfg.run(cc, []string{a.ID, b.ID}); err != nil {
		t.Fatalf("third run: %v", err)
	}
	if !strings.Contains(out.String(), "already has") {
		t.Errorf("third run printed %q; both halves are there", strings.TrimSpace(out.String()))
	}
}
