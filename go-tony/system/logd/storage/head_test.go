package storage

import (
	"fmt"
	"math/rand"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// The head is a third way of computing state, alongside the two the read-path oracle
// already compares: it steps the notification's stripped patch with tony.Patch, where the
// read path applies the stored, still-tagged entries through the streaming processor.
//
// So it is checked against the same reference the oracle calls the semantics of record —
// a storage that never snapshots, whose every read folds from commit 0. Comparing against
// that rather than against the subject's own reads means a shared misconception in the
// snapshot path cannot hide a divergence here.
//
// Reads are compared at the root, since that is what the head holds; the paths below it
// are the oracle's job.
func TestHeadEquivalence_SteppedVsReference(t *testing.T) {
	for seed := 1; seed <= seedCount(); seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			runHeadEquivalenceSeed(t, int64(seed), 40)
		})
	}
}

func runHeadEquivalenceSeed(t *testing.T, seed int64, nOps int) {
	rng := rand.New(rand.NewSource(seed))
	ops := genOps(rng, nOps)

	ref := openTestStorage(t)  // never snapshots: every read folds from commit 0
	subj := openTestStorage(t) // steps a head, and snapshots at the generated points

	// Seed the head before any write, so every commit in the stream is a step rather
	// than a re-seed. Lazily seeding would still be correct, but it would test the
	// seed far more than the stepping.
	seedHead(t, subj)

	for i, o := range ops {
		refCommit, refErr := applyOp(t, ref, o)
		subjCommit, subjErr := applyOp(t, subj, o)
		if (refErr == nil) != (subjErr == nil) {
			t.Fatalf("seed %d op %d %s: commit disagreed: reference %v, subject %v",
				seed, i, o, refErr, subjErr)
		}
		if refErr != nil {
			continue
		}
		if refCommit != subjCommit {
			t.Fatalf("seed %d op %d %s: commit numbers diverged: %d vs %d",
				seed, i, o, refCommit, subjCommit)
		}

		// The head must equal the fold at every commit, not only at the end: a step
		// that is wrong and a later step that is wrong the other way would cancel.
		want := read(ref, "", refCommit)
		if want.err != nil {
			t.Fatalf("seed %d op %d: reference read: %v", seed, i, want.err)
		}
		got, gotCommit := headOf(subj)
		if gotCommit != subjCommit {
			t.Fatalf("seed %d op %d %s: head is at commit %d, want %d (a step was skipped or dropped)",
				seed, i, o, gotCommit, subjCommit)
		}
		if !nodeEqual(got, want.node) {
			t.Fatalf("seed %d op %d %s: head diverged at commit %d\n head: %s\n want: %s",
				seed, i, o, subjCommit, nodeText(got), nodeText(want.node))
		}

		if o.snapshot {
			if err := subj.SwitchDLog(); err != nil {
				t.Fatalf("seed %d op %d: SwitchDLog: %v", seed, i, err)
			}
			// SwitchDLog checks the head against a full read and drops it on
			// disagreement, so surviving the switch is itself the assertion.
			if _, c := headOf(subj); c != subjCommit {
				t.Fatalf("seed %d op %d: the snapshot check dropped the head at commit %d",
					seed, i, subjCommit)
			}
		}
	}
}

// A dropped head must be re-seeded rather than answer from stale state, and the answer
// after re-seeding must be the one a full read gives.
func TestHeadReseedsAfterDrop(t *testing.T) {
	s := openTestStorage(t)
	seedHead(t, s)

	for _, src := range []string{"a: 1\n", "b:\n  c: 2\n", "a: 9\n"} {
		writeDoc(t, s, src)
	}
	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatal(err)
	}
	want, err := s.replayBaselineAt(commit)
	if err != nil {
		t.Fatal(err)
	}

	s.commitMu.Lock()
	s.dropHead("test", nil)
	if s.headSeeded {
		t.Fatal("dropHead left a head behind")
	}
	got, err := s.steppedBaselineAt(commit)
	s.commitMu.Unlock()
	if err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	if !nodeEqual(got, want) {
		t.Errorf("re-seeded head is not the full read\n got: %s\nwant: %s", nodeText(got), nodeText(want))
	}
}

// A step that cannot be applied must drop the head, not keep a half-applied one. A real
// commit's patch is not expected to fail, so the step is driven directly with one that
// does: an arraydiff against a number, which the op refuses.
func TestHeadDropsOnStepFailure(t *testing.T) {
	s := openTestStorage(t)
	seedHead(t, s)
	writeDoc(t, s, "a: 1\n")

	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	if !s.headSeeded {
		t.Fatal("no head to step")
	}
	bad, err := parse.Parse([]byte("a:\n  !arraydiff\n  0: !insert 5\n"))
	if err != nil {
		t.Fatal(err)
	}
	// Confirm the patch really does fail, so the test is not passing on a typo.
	if _, err := tony.Patch(s.head, bad); err == nil {
		t.Fatal("expected the patch to fail")
	}
	s.stepHead(s.headCommit+1, bad)
	if s.headSeeded {
		t.Error("a failed step left the head in place")
	}
}

// A gap — a commit that did not step the head — must drop it rather than skip a commit's
// worth of state.
func TestHeadDropsOnCommitGap(t *testing.T) {
	s := openTestStorage(t)
	seedHead(t, s)
	writeDoc(t, s, "a: 1\n")

	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	patch, err := parse.Parse([]byte("b: 2\n"))
	if err != nil {
		t.Fatal(err)
	}
	s.stepHead(s.headCommit+2, patch) // skips one
	if s.headSeeded {
		t.Error("a commit gap left the head in place")
	}
}

// A scoped write takes a commit number but is not part of baseline state, so the head has
// to follow the number without applying the patch. Getting this wrong reads as a gap and
// drops the head on every scoped write.
func TestHeadFollowsScopedCommits(t *testing.T) {
	s := openTestStorage(t)
	seedHead(t, s)
	writeDoc(t, s, "a: 1\n")

	scope := "s1"
	writeDocScoped(t, s, "a: 999\n", &scope)

	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatal(err)
	}
	head, headCommit := headOf(s)
	if head == nil {
		t.Fatal("the scoped write dropped the head")
	}
	if headCommit != commit {
		t.Errorf("head is at commit %d, want %d", headCommit, commit)
	}
	want, err := s.replayBaselineAt(commit)
	if err != nil {
		t.Fatal(err)
	}
	if !nodeEqual(head, want) {
		t.Errorf("the scoped write leaked into the baseline head\n got: %s\nwant: %s",
			nodeText(head), nodeText(want))
	}
}

func seedHead(t *testing.T, s *Storage) {
	t.Helper()
	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatal(err)
	}
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	if _, err := s.steppedBaselineAt(commit); err != nil {
		t.Fatalf("seed head: %v", err)
	}
}

func headOf(s *Storage) (*ir.Node, int64) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	return s.head, s.headCommit
}

func writeDoc(t *testing.T, s *Storage, src string) {
	t.Helper()
	writeDocScoped(t, s, src, nil)
}

func writeDocScoped(t *testing.T, s *Storage, src string, scope *string) {
	t.Helper()
	n, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	txn, err := s.NewTx(1, scope)
	if err != nil {
		t.Fatalf("write %q: %v", src, err)
	}
	p, err := txn.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: n}})
	if err != nil {
		t.Fatalf("write %q: %v", src, err)
	}
	if res := p.Commit(); !res.Committed {
		t.Fatalf("write %q: %v", src, res.Error)
	}
}
