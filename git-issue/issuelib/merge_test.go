package issuelib

import (
	"strings"
	"testing"
)

// forkSides makes an issue and a second ref at the same commit, so a test can
// edit each side independently: the shape two clones are in when both have
// edited one issue.
func forkSides(t *testing.T, s *GitStore) (issue *Issue, base, theirs string) {
	t.Helper()
	issue, err := s.Create("subject", "# subject\n\nbody\n")
	if err != nil {
		t.Fatal(err)
	}
	base = refAtOrEmpty(t, issue.Ref)
	theirs = "refs/test/theirs/" + issue.ID
	putRef(t, theirs, base)
	return issue, base, theirs
}

// edit rewrites one side's metadata, at a committer date the test chooses so
// that the order of two people's decisions is the thing being tested.
func edit(t *testing.T, s *GitStore, ref, when string, change func(*Issue)) {
	t.Helper()
	issue, _, err := s.GetByRef(ref)
	if err != nil {
		t.Fatalf("read %s: %v", ref, err)
	}
	change(issue)
	if when != "" {
		t.Setenv("GIT_COMMITTER_DATE", when)
		defer t.Setenv("GIT_COMMITTER_DATE", "")
	}
	if err := s.Update(issue, "edit", nil); err != nil {
		t.Fatalf("edit %s: %v", ref, err)
	}
}

func closes(sha string) func(*Issue) {
	return func(i *Issue) { i.Status = "closed"; i.ClosedBy = &sha }
}

func reopens(i *Issue) { i.Status = "open"; i.ClosedBy = nil }

// TestMergeIssue_ListsUnion: what two clones recorded about one issue is both of
// those things. Order is ours, then what only theirs has, so a merge does not
// reshuffle what someone is reading.
func TestMergeIssue_ListsUnion(t *testing.T) {
	gitInit(t)
	s := NewGitStoreWithOutput(&strings.Builder{})
	issue, base, theirs := forkSides(t, s)

	edit(t, s, issue.Ref, "", func(i *Issue) {
		i.Labels = []string{"bug", "urgent"}
		i.RelatedIssues = []string{"aaa"}
	})
	edit(t, s, theirs, "", func(i *Issue) {
		i.Labels = []string{"urgent", "docs"}
		i.RelatedIssues = []string{"bbb"}
	})

	merged, err := s.MergeIssue(base, refAtOrEmpty(t, issue.Ref), refAtOrEmpty(t, theirs))
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	got, err := s.metaAt(merged)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"bug", "urgent", "docs"}; !equalStrings(got.Labels, want) {
		t.Errorf("labels %v, want %v", got.Labels, want)
	}
	if want := []string{"aaa", "bbb"}; !equalStrings(got.RelatedIssues, want) {
		t.Errorf("related %v, want %v", got.RelatedIssues, want)
	}
	if got.ID != issue.ID {
		t.Errorf("id %q, want %q", got.ID, issue.ID)
	}
}

// TestMergeIssue_TheLaterStatusChangeWins: whoever acted second acted knowing
// more. A reopen after a close means someone looked again, and closing it back
// would be this tool overruling them; a close after a reopen is the same in
// reverse. A side that changed nothing does not compete, and a tie closes.
func TestMergeIssue_TheLaterStatusChangeWins(t *testing.T) {
	const earlier = "2026-09-01T10:00:00+00:00"
	const later = "2026-09-02T10:00:00+00:00"

	for _, tc := range []struct {
		name       string
		ours       func(t *testing.T, s *GitStore, ref string)
		theirs     func(t *testing.T, s *GitStore, ref string)
		wantStatus string
		wantClosed bool
	}{
		{
			name:       "neither side touched it",
			ours:       func(t *testing.T, s *GitStore, ref string) { edit(t, s, ref, "", func(i *Issue) {}) },
			theirs:     func(t *testing.T, s *GitStore, ref string) { edit(t, s, ref, "", func(i *Issue) {}) },
			wantStatus: "open",
		},
		{
			name:       "only this side closed it",
			ours:       func(t *testing.T, s *GitStore, ref string) { edit(t, s, ref, earlier, closes("c0ffee")) },
			theirs:     func(t *testing.T, s *GitStore, ref string) { edit(t, s, ref, later, func(i *Issue) {}) },
			wantStatus: "closed",
			wantClosed: true,
		},
		{
			name:       "only the other side closed it",
			ours:       func(t *testing.T, s *GitStore, ref string) { edit(t, s, ref, later, func(i *Issue) {}) },
			theirs:     func(t *testing.T, s *GitStore, ref string) { edit(t, s, ref, earlier, closes("c0ffee")) },
			wantStatus: "closed",
			wantClosed: true,
		},
		{
			name: "a reopen after a close",
			ours: func(t *testing.T, s *GitStore, ref string) { edit(t, s, ref, earlier, closes("c0ffee")) },
			theirs: func(t *testing.T, s *GitStore, ref string) {
				edit(t, s, ref, earlier, closes("c0ffee"))
				edit(t, s, ref, later, reopens)
			},
			wantStatus: "open",
		},
		{
			name: "a close after a reopen",
			ours: func(t *testing.T, s *GitStore, ref string) {
				edit(t, s, ref, earlier, closes("c0ffee"))
				edit(t, s, ref, earlier, reopens)
			},
			theirs:     func(t *testing.T, s *GitStore, ref string) { edit(t, s, ref, later, closes("dec1de")) },
			wantStatus: "closed",
			wantClosed: true,
		},
		{
			name:       "both at once, which closes",
			ours:       func(t *testing.T, s *GitStore, ref string) { edit(t, s, ref, earlier, reopens) },
			theirs:     func(t *testing.T, s *GitStore, ref string) { edit(t, s, ref, earlier, closes("dec1de")) },
			wantStatus: "closed",
			wantClosed: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gitInit(t)
			s := NewGitStoreWithOutput(&strings.Builder{})
			issue, base, theirs := forkSides(t, s)
			tc.ours(t, s, issue.Ref)
			tc.theirs(t, s, theirs)

			merged, err := s.MergeIssue(base, refAtOrEmpty(t, issue.Ref), refAtOrEmpty(t, theirs))
			if err != nil {
				t.Fatalf("merge: %v", err)
			}
			got, err := s.metaAt(merged)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.wantStatus {
				t.Errorf("status %q, want %q", got.Status, tc.wantStatus)
			}
			if (got.ClosedBy != nil) != tc.wantClosed {
				t.Errorf("closed_by %v, and the issue is %s", got.ClosedBy, got.Status)
			}
		})
	}
}

// TestAdoptGen0_MergesAPairEditedOnBothSidesOfAnUpgrade: someone ran the older
// binary after the newer one and edited the issue in both. Neither rename is
// right -- each would drop the other's commits -- so the two chains are brought
// together and the older ref is cleared like any other.
func TestAdoptGen0_MergesAPairEditedOnBothSidesOfAnUpgrade(t *testing.T) {
	gitInit(t)
	s := NewGitStoreWithOutput(&strings.Builder{})
	issue, err := s.Create("both ways", "# both ways\n")
	if err != nil {
		t.Fatal(err)
	}
	base := refAtOrEmpty(t, issue.Ref)
	putRef(t, Gen0RefForXIDR(issue.ID), base)
	if err := s.updateCommit(issue.Ref, "ours", map[string]string{"discussion/ours.md": "ours\n"}); err != nil {
		t.Fatal(err)
	}
	if err := s.updateCommit(Gen0RefForXIDR(issue.ID), "theirs", map[string]string{"discussion/theirs.md": "theirs\n"}); err != nil {
		t.Fatal(err)
	}

	var said strings.Builder
	if err := NewGitStoreWithOutput(&said).AdoptGen0(); err != nil {
		t.Fatalf("adopt: %v", err)
	}

	if left := refsAt(Gen0OpenPrefix+"*", Gen0ClosedPrefix+"*"); len(left) != 0 {
		t.Errorf("the older ref is still here: %v", left)
	}
	if !strings.Contains(said.String(), FormatID(issue.ID)) {
		t.Errorf("the merge was not reported: %q", said.String())
	}
	files, err := s.ListDir(issue.Ref, "discussion")
	if err != nil {
		t.Fatalf("read the discussion: %v", err)
	}
	for _, want := range []string{"ours.md", "theirs.md"} {
		if _, ok := files[want]; !ok {
			t.Errorf("the merge does not hold %s: %v", want, files)
		}
	}
}
