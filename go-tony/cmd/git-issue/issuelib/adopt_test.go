package issuelib

import (
	"os/exec"
	"strings"
	"testing"
)

// putRef points ref at commit with no questions asked, as a client of another
// generation would have left it.
func putRef(t *testing.T, ref, commit string) {
	t.Helper()
	if out, err := exec.Command("git", "update-ref", ref, commit).CombinedOutput(); err != nil {
		t.Fatalf("update-ref %s: %v: %s", ref, err, out)
	}
}

func dropRef(t *testing.T, ref string) {
	t.Helper()
	if out, err := exec.Command("git", "update-ref", "-d", ref).CombinedOutput(); err != nil {
		t.Fatalf("update-ref -d %s: %v: %s", ref, err, out)
	}
}

func refAtOrEmpty(t *testing.T, ref string) string {
	t.Helper()
	if found := refsAt(ref); len(found) == 1 {
		return found[0].commit
	}
	return ""
}

// TestAdoptGen0_TakesOverACloneAnOlderBinaryWrote: an issue left where the
// layout before this generation kept it is readable here, at the commit it was
// already at -- no commit is rewritten, so what was recorded about it elsewhere
// still resolves.
func TestAdoptGen0_TakesOverACloneAnOlderBinaryWrote(t *testing.T) {
	gitInit(t)
	s := NewGitStoreWithOutput(&strings.Builder{})

	open, err := s.Create("open one", "# open one\n")
	if err != nil {
		t.Fatal(err)
	}
	closed, err := s.Create("closed one", "# closed one\n")
	if err != nil {
		t.Fatal(err)
	}
	openAt := refAtOrEmpty(t, open.Ref)
	closedAt := refAtOrEmpty(t, closed.Ref)

	// As an older client left them.
	putRef(t, Gen0RefForXIDR(open.ID), openAt)
	dropRef(t, open.Ref)
	putRef(t, Gen0ClosedRefForXIDR(closed.ID), closedAt)
	dropRef(t, closed.Ref)

	fresh := NewGitStoreWithOutput(&strings.Builder{})
	if err := fresh.AdoptGen0(); err != nil {
		t.Fatalf("adopt: %v", err)
	}

	if got := refAtOrEmpty(t, RefForXIDR(open.ID)); got != openAt {
		t.Errorf("the open issue is at %q, want %q", got, openAt)
	}
	if got := refAtOrEmpty(t, ClosedRefForXIDR(closed.ID)); got != closedAt {
		t.Errorf("the closed issue is at %q, want %q", got, closedAt)
	}
	if left := refsAt(Gen0OpenPrefix+"*", Gen0ClosedPrefix+"*"); len(left) != 0 {
		t.Errorf("refs of the older layout are still here: %v", left)
	}

	// And they read as issues, status and all.
	if issue, _, err := fresh.Get(open.ID); err != nil || issue.Ref != RefForXIDR(open.ID) {
		t.Errorf("the open issue does not read: %+v, %v", issue, err)
	}
	if issue, _, err := fresh.Get(closed.ID); err != nil || !IsClosedRef(issue.Ref) {
		t.Errorf("the closed issue does not read as closed: %+v, %v", issue, err)
	}

	// Idempotent, and quiet the second time.
	var said strings.Builder
	quiet := NewGitStoreWithOutput(&said)
	if err := quiet.AdoptGen0(); err != nil {
		t.Fatalf("second adopt: %v", err)
	}
	if said.String() != "" {
		t.Errorf("a second adoption said %q, want nothing", said.String())
	}
}

// TestAdoptGen0_AgainstWhatThisGenerationHolds: the gen0 ref and a ref here can
// both exist -- someone ran the old binary after the new one -- and which is the
// later of the two decides. Only a genuine divergence is left for a sync to
// settle.
func TestAdoptGen0_AgainstWhatThisGenerationHolds(t *testing.T) {
	for _, tc := range []struct {
		name string
		// set up leaves the store holding whatever the case needs, and answers
		// the commit the issue should be at afterwards, and where.
		run func(t *testing.T, s *GitStore, issue *Issue) (wantRef, wantCommit string)
	}{
		{"nothing held here", func(t *testing.T, s *GitStore, issue *Issue) (string, string) {
			at := refAtOrEmpty(t, issue.Ref)
			putRef(t, Gen0RefForXIDR(issue.ID), at)
			dropRef(t, issue.Ref)
			return RefForXIDR(issue.ID), at
		}},
		{"what is held is the same commit", func(t *testing.T, s *GitStore, issue *Issue) (string, string) {
			at := refAtOrEmpty(t, issue.Ref)
			putRef(t, Gen0RefForXIDR(issue.ID), at)
			return RefForXIDR(issue.ID), at
		}},
		{"what is held carries the gen0 tip", func(t *testing.T, s *GitStore, issue *Issue) (string, string) {
			old := refAtOrEmpty(t, issue.Ref)
			putRef(t, Gen0RefForXIDR(issue.ID), old)
			if err := s.updateCommit(issue.Ref, "later", map[string]string{"discussion/a.md": "a\n"}); err != nil {
				t.Fatal(err)
			}
			return RefForXIDR(issue.ID), refAtOrEmpty(t, issue.Ref)
		}},
		{"the gen0 tip is ahead", func(t *testing.T, s *GitStore, issue *Issue) (string, string) {
			old := refAtOrEmpty(t, issue.Ref)
			if err := s.updateCommit(issue.Ref, "later", map[string]string{"discussion/a.md": "a\n"}); err != nil {
				t.Fatal(err)
			}
			ahead := refAtOrEmpty(t, issue.Ref)
			putRef(t, Gen0RefForXIDR(issue.ID), ahead)
			putRef(t, issue.Ref, old)
			return RefForXIDR(issue.ID), ahead
		}},
		{"the gen0 tip is ahead and closed", func(t *testing.T, s *GitStore, issue *Issue) (string, string) {
			old := refAtOrEmpty(t, issue.Ref)
			if err := s.updateCommit(issue.Ref, "later", map[string]string{"discussion/a.md": "a\n"}); err != nil {
				t.Fatal(err)
			}
			ahead := refAtOrEmpty(t, issue.Ref)
			putRef(t, Gen0ClosedRefForXIDR(issue.ID), ahead)
			putRef(t, issue.Ref, old)
			return ClosedRefForXIDR(issue.ID), ahead
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gitInit(t)
			s := NewGitStoreWithOutput(&strings.Builder{})
			issue, err := s.Create("subject", "# subject\n")
			if err != nil {
				t.Fatal(err)
			}
			wantRef, wantCommit := tc.run(t, s, issue)

			if err := NewGitStoreWithOutput(&strings.Builder{}).AdoptGen0(); err != nil {
				t.Fatalf("adopt: %v", err)
			}

			if got := refAtOrEmpty(t, wantRef); got != wantCommit {
				t.Errorf("%s is at %q, want %q", wantRef, got, wantCommit)
			}
			if left := refsAt(Gen0OpenPrefix+"*", Gen0ClosedPrefix+"*"); len(left) != 0 {
				t.Errorf("refs of the older layout are still here: %v", left)
			}
			// One ref for the issue, in one status.
			if held := refsAt(RefForXIDR(issue.ID), ClosedRefForXIDR(issue.ID)); len(held) != 1 {
				t.Errorf("the issue is held by %d refs, want 1: %v", len(held), held)
			}
		})
	}
}

// TestAdoptGen0_FoldsTheOlderReverseIndex: the commit -> issue index of the
// older layout is merged into this one's rather than dropped, so a link made by
// the older binary still answers. The two are unrelated histories, which the
// union strategy merges all the same.
func TestAdoptGen0_FoldsTheOlderReverseIndex(t *testing.T) {
	gitInit(t)
	s := NewGitStoreWithOutput(&strings.Builder{})
	if out, err := exec.Command("git", "commit", "-q", "--allow-empty", "-m", "root").CombinedOutput(); err != nil {
		t.Fatalf("commit: %v: %s", err, out)
	}
	head, err := s.VerifyCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}

	// A note the older binary left, and one this generation made.
	if out, err := exec.Command("git", "notes", "--ref="+Gen0NotesRef, "add", "-m", "older-issue", head).CombinedOutput(); err != nil {
		t.Fatalf("notes add: %v: %s", err, out)
	}
	if err := s.AddNote(head, "newer-issue"); err != nil {
		t.Fatal(err)
	}

	fresh := NewGitStoreWithOutput(&strings.Builder{})
	if err := fresh.AdoptGen0(); err != nil {
		t.Fatalf("adopt: %v", err)
	}

	note, err := fresh.GetNotes(head)
	if err != nil {
		t.Fatalf("the reverse index does not answer: %v", err)
	}
	for _, want := range []string{"older-issue", "newer-issue"} {
		if !strings.Contains(note, want) {
			t.Errorf("the merged index does not hold %q: %q", want, note)
		}
	}
	if left := refsAt(Gen0NotesRef); len(left) != 0 {
		t.Errorf("the older reverse index is still here: %v", left)
	}
}

// TestAdoptGen0_OutsideARepositoryDoesNothing: every command builds a store
// before it knows where it is running, and a directory that is not a repository
// must not be an error on the way to the real one.
func TestAdoptGen0_OutsideARepositoryDoesNothing(t *testing.T) {
	t.Chdir(t.TempDir())
	var said strings.Builder
	s := NewGitStoreWithOutput(&said)
	if err := s.AdoptGen0(); err != nil {
		t.Errorf("adoption outside a repository: %v", err)
	}
	if said.String() != "" {
		t.Errorf("it said %q, want nothing", said.String())
	}
}
