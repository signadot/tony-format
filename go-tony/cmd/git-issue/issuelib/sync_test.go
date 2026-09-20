package issuelib

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// chain makes an issue and answers n commits of it, oldest first, so a test can
// place either side at any point of one history.
func chain(t *testing.T, s *GitStore, n int) (*Issue, []string) {
	t.Helper()
	issue, err := s.Create("subject", "# subject\n")
	if err != nil {
		t.Fatal(err)
	}
	commits := []string{refAtOrEmpty(t, issue.Ref)}
	for i := 1; i < n; i++ {
		if err := s.updateCommit(issue.Ref, fmt.Sprintf("edit %d", i),
			map[string]string{fmt.Sprintf("discussion/%d.md", i): "x\n"}); err != nil {
			t.Fatal(err)
		}
		commits = append(commits, refAtOrEmpty(t, issue.Ref))
	}
	return issue, commits
}

// fork commits once on ref, which a test has put at an earlier commit, so that
// the two sides of an issue hold work the other does not.
func fork(t *testing.T, s *GitStore, ref, note string) string {
	t.Helper()
	if err := s.updateCommit(ref, note, map[string]string{"discussion/" + note + ".md": note + "\n"}); err != nil {
		t.Fatal(err)
	}
	return refAtOrEmpty(t, ref)
}

// planFor answers the plan for one issue, which is the only one these tests make.
func planFor(t *testing.T, s *GitStore, remote, xidr string) IssuePlan {
	t.Helper()
	plans, err := s.PlanSync(remote)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for _, p := range plans {
		if p.XIDR == xidr {
			return p
		}
	}
	t.Fatalf("no plan for %s among %d", xidr, len(plans))
	return IssuePlan{}
}

// TestPlanSync_Verdicts: what each side holds decides what a sync may do, and
// the decision is made from refs and ancestry alone -- no network, nothing
// written -- which is why it can be driven by putting refs where a fetch would
// have.
func TestPlanSync_Verdicts(t *testing.T) {
	const remote = "origin"
	open := func(xidr string) string { return TrackingOpenPrefix(remote) + xidr }
	closed := func(xidr string) string { return TrackingClosedPrefix(remote) + xidr }
	gen0Open := func(xidr string) string { return TrackingGen0OpenPrefix(remote) + xidr }
	gen0Closed := func(xidr string) string { return TrackingGen0ClosedPrefix(remote) + xidr }

	for _, tc := range []struct {
		name string
		// setUp places the two sides and answers what the verdict should be, the
		// commit R should be at ("" for none), and whether the remote is split.
		setUp func(t *testing.T, s *GitStore, issue *Issue, c []string) (Verdict, string, bool)
	}{
		{"remote only", func(t *testing.T, s *GitStore, issue *Issue, c []string) (Verdict, string, bool) {
			putRef(t, open(issue.ID), c[0])
			dropRef(t, issue.Ref)
			return RemoteOnly, c[0], false
		}},
		{"local only", func(t *testing.T, s *GitStore, issue *Issue, c []string) (Verdict, string, bool) {
			return LocalOnly, "", false
		}},
		{"equal", func(t *testing.T, s *GitStore, issue *Issue, c []string) (Verdict, string, bool) {
			putRef(t, open(issue.ID), c[1])
			putRef(t, issue.Ref, c[1])
			return Equal, c[1], false
		}},
		{"behind", func(t *testing.T, s *GitStore, issue *Issue, c []string) (Verdict, string, bool) {
			putRef(t, open(issue.ID), c[1])
			putRef(t, issue.Ref, c[0])
			return Behind, c[1], false
		}},
		{"ahead", func(t *testing.T, s *GitStore, issue *Issue, c []string) (Verdict, string, bool) {
			putRef(t, open(issue.ID), c[0])
			return Ahead, c[0], false
		}},
		{"diverged", func(t *testing.T, s *GitStore, issue *Issue, c []string) (Verdict, string, bool) {
			putRef(t, open(issue.ID), c[0])
			theirs := fork(t, s, open(issue.ID), "theirs")
			return Diverged, theirs, false
		}},
		{"the remote holds one commit in both namespaces", func(t *testing.T, s *GitStore, issue *Issue, c []string) (Verdict, string, bool) {
			putRef(t, open(issue.ID), c[1])
			putRef(t, closed(issue.ID), c[1])
			putRef(t, issue.Ref, c[1])
			// The same chain, and one of the two says it is finished.
			return Equal, c[1], false
		}},
		{"the remote's tips are a chain across namespaces", func(t *testing.T, s *GitStore, issue *Issue, c []string) (Verdict, string, bool) {
			putRef(t, open(issue.ID), c[0])
			putRef(t, closed(issue.ID), c[1])
			putRef(t, issue.Ref, c[1])
			return Equal, c[1], false
		}},
		{"the remote holds all four", func(t *testing.T, s *GitStore, issue *Issue, c []string) (Verdict, string, bool) {
			putRef(t, open(issue.ID), c[0])
			putRef(t, closed(issue.ID), c[1])
			putRef(t, gen0Open(issue.ID), c[0])
			putRef(t, gen0Closed(issue.ID), c[2])
			putRef(t, issue.Ref, c[2])
			return Equal, c[2], false
		}},
		{"the remote's own tips disagree", func(t *testing.T, s *GitStore, issue *Issue, c []string) (Verdict, string, bool) {
			putRef(t, open(issue.ID), c[1])
			putRef(t, closed(issue.ID), c[0])
			fork(t, s, closed(issue.ID), "theirs")
			return Diverged, "", true
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gitInit(t)
			s := NewGitStoreWithOutput(&strings.Builder{})
			issue, commits := chain(t, s, 3)
			wantVerdict, wantR, wantSplit := tc.setUp(t, s, issue, commits)

			p := planFor(t, s, remote, issue.ID)
			if p.Verdict != wantVerdict {
				t.Errorf("verdict %v, want %v", p.Verdict, wantVerdict)
			}
			if p.Split != wantSplit {
				t.Errorf("split %v, want %v", p.Split, wantSplit)
			}
			switch {
			case wantR == "" && p.R != nil:
				t.Errorf("the remote's tip is %s, and it should have none", shortSHA(p.R.Commit))
			case wantR != "" && p.R == nil:
				t.Errorf("the remote has no tip, want %s", shortSHA(wantR))
			case wantR != "" && p.R.Commit != wantR:
				t.Errorf("the remote's tip is %s, want %s", shortSHA(p.R.Commit), shortSHA(wantR))
			}
		})
	}
}

// TestPlanSync_ClosedWinsATie: one commit in both of a remote's namespaces is an
// issue that was closed, and the close is the news.
func TestPlanSync_ClosedWinsATie(t *testing.T) {
	gitInit(t)
	s := NewGitStoreWithOutput(&strings.Builder{})
	issue, commits := chain(t, s, 1)
	putRef(t, TrackingOpenPrefix("origin")+issue.ID, commits[0])
	putRef(t, TrackingClosedPrefix("origin")+issue.ID, commits[0])

	p := planFor(t, s, "origin", issue.ID)
	if p.R == nil || !p.R.Closed {
		t.Fatalf("the remote's tip is %+v, want the closed one", p.R)
	}
	// And a pull takes the status even though the chain has not moved.
	if _, err := s.ApplyPull(p, false); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if got := refAtOrEmpty(t, ClosedRefForXIDR(issue.ID)); got != commits[0] {
		t.Errorf("the issue is not closed here: %q", got)
	}
	if got := refAtOrEmpty(t, RefForXIDR(issue.ID)); got != "" {
		t.Errorf("the issue is open here as well, at %s", shortSHA(got))
	}
}

// TestPlanSync_OldClientOnlyOnAMigratedRemote: a gen0 ref on a remote that has
// otherwise moved to this generation was pushed by a client too old to see the
// current refs. On a remote that has not moved, gen0 is simply where its issues
// are, and there is nothing to report.
func TestPlanSync_OldClientOnlyOnAMigratedRemote(t *testing.T) {
	for _, tc := range []struct {
		name      string
		migrated  bool
		oldClient bool
	}{
		{"a remote still on the older layout", false, false},
		{"a remote that has moved", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gitInit(t)
			s := NewGitStoreWithOutput(&strings.Builder{})
			issue, commits := chain(t, s, 1)
			putRef(t, TrackingGen0OpenPrefix("origin")+issue.ID, commits[0])
			if tc.migrated {
				other, otherCommits := chain(t, s, 1)
				putRef(t, TrackingOpenPrefix("origin")+other.ID, otherCommits[0])
			}
			if got := planFor(t, s, "origin", issue.ID).OldClient; got != tc.oldClient {
				t.Errorf("OldClient = %v, want %v", got, tc.oldClient)
			}
		})
	}
}

// TestApplyPull_RefusesADivergenceUnlessForced: the loss this whole file is
// about. Neither side's chain carries the other, so taking either silently
// drops work; the caller is told instead, and says which side wins if it wants
// one.
func TestApplyPull_RefusesADivergenceUnlessForced(t *testing.T) {
	gitInit(t)
	s := NewGitStoreWithOutput(&strings.Builder{})
	issue, commits := chain(t, s, 2)
	putRef(t, TrackingOpenPrefix("origin")+issue.ID, commits[0])
	theirs := fork(t, s, TrackingOpenPrefix("origin")+issue.ID, "theirs")

	p := planFor(t, s, "origin", issue.ID)
	if p.Verdict != Diverged {
		t.Fatalf("verdict %v, want %v", p.Verdict, Diverged)
	}
	if _, err := s.ApplyPull(p, false); err == nil {
		t.Error("a divergence was applied without being asked twice")
	}
	if got := refAtOrEmpty(t, issue.Ref); got != commits[1] {
		t.Errorf("the refused pull moved the ref to %s", shortSHA(got))
	}

	if _, err := s.ApplyPull(p, true); err != nil {
		t.Fatalf("forced pull: %v", err)
	}
	if got := refAtOrEmpty(t, issue.Ref); got != theirs {
		t.Errorf("the forced pull left the ref at %s, want %s", shortSHA(got), shortSHA(theirs))
	}
	// What it overwrote is still reachable, which is what makes force survivable.
	if logged := reflogTips(t, issue.Ref); !contains(logged, commits[1]) {
		t.Errorf("the overwritten tip is not in the reflog: %v", logged)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// putRemote puts commit at ref on the remote, as another client would have.
func putRemote(t *testing.T, remote, ref, commit string) {
	t.Helper()
	if out, err := exec.Command("git", "push", remote, commit+":"+ref).CombinedOutput(); err != nil {
		t.Fatalf("push %s to %s: %v: %s", ref, remote, err, out)
	}
}

// remoteRefsOf is every ref the remote holds, sorted.
func remoteRefsOf(t *testing.T, remote string) []string {
	t.Helper()
	out, err := exec.Command("git", "ls-remote", "--refs", remote).Output()
	if err != nil {
		t.Fatalf("ls-remote %s: %v", remote, err)
	}
	var refs []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if _, ref, ok := strings.Cut(line, "\t"); ok {
			refs = append(refs, ref)
		}
	}
	sort.Strings(refs)
	return refs
}

// TestApplyPush_MakesTheRemoteRight: the remote ends holding exactly one ref for
// the issue, this generation's, in the status this clone has it in. So one rule
// sends the issue, mirrors a close, and migrates an issue the remote only ever
// had in the older layout -- which is why there is no migration command.
func TestApplyPush_MakesTheRemoteRight(t *testing.T) {
	for _, tc := range []struct {
		name string
		// setUp leaves the remote as another client had it, and answers the refs
		// the remote should hold once the push has run.
		setUp func(t *testing.T, s *GitStore, issue *Issue, c []string) []string
	}{
		{"creates what the remote does not have", func(t *testing.T, s *GitStore, issue *Issue, c []string) []string {
			return []string{RefForXIDR(issue.ID)}
		}},
		{"carries the issue forward", func(t *testing.T, s *GitStore, issue *Issue, c []string) []string {
			putRemote(t, "origin", RefForXIDR(issue.ID), c[0])
			return []string{RefForXIDR(issue.ID)}
		}},
		{"mirrors a close", func(t *testing.T, s *GitStore, issue *Issue, c []string) []string {
			putRemote(t, "origin", RefForXIDR(issue.ID), c[1])
			if err := s.MoveRef(issue.Ref, ClosedRefForXIDR(issue.ID)); err != nil {
				t.Fatal(err)
			}
			return []string{ClosedRefForXIDR(issue.ID)}
		}},
		{"migrates an issue the remote has in the older layout", func(t *testing.T, s *GitStore, issue *Issue, c []string) []string {
			putRemote(t, "origin", Gen0RefForXIDR(issue.ID), c[1])
			return []string{RefForXIDR(issue.ID)}
		}},
		{"migrates a closed one, and drops both older refs", func(t *testing.T, s *GitStore, issue *Issue, c []string) []string {
			putRemote(t, "origin", Gen0RefForXIDR(issue.ID), c[0])
			putRemote(t, "origin", Gen0ClosedRefForXIDR(issue.ID), c[1])
			if err := s.MoveRef(issue.Ref, ClosedRefForXIDR(issue.ID)); err != nil {
				t.Fatal(err)
			}
			return []string{ClosedRefForXIDR(issue.ID)}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gitInitWithOrigin(t)
			s := NewGitStoreWithOutput(&strings.Builder{})
			issue, commits := chain(t, s, 2)
			want := tc.setUp(t, s, issue, commits)

			if _, err := s.FetchTracking("origin"); err != nil {
				t.Fatalf("fetch: %v", err)
			}
			p := planFor(t, s, "origin", issue.ID)
			if _, err := s.ApplyPush("origin", p, false); err != nil {
				t.Fatalf("push: %v", err)
			}
			if got := remoteRefsOf(t, "origin"); !equalStrings(got, want) {
				t.Errorf("the remote holds %v, want %v", got, want)
			}
		})
	}
}

// TestApplyPush_LeaseRefusesAWriteOverSomeoneElse: between a fetch and a push,
// someone else pushed. The plan was made against what the remote held then, so
// every ref it writes carries a lease on that -- and the push is refused whole
// rather than overwriting work this clone has never seen. That is the
// compare-and-swap the store has always made locally, at last reaching the wire.
func TestApplyPush_LeaseRefusesAWriteOverSomeoneElse(t *testing.T) {
	gitInitWithOrigin(t)
	s := NewGitStoreWithOutput(&strings.Builder{})
	issue, commits := chain(t, s, 2)
	putRemote(t, "origin", RefForXIDR(issue.ID), commits[0])

	if _, err := s.FetchTracking("origin"); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	p := planFor(t, s, "origin", issue.ID)
	if p.Verdict != Ahead {
		t.Fatalf("verdict %v, want %v", p.Verdict, Ahead)
	}

	// Someone else gets there first, with work this clone does not have.
	elsewhere := ClosedRefForXIDR(issue.ID) + "-elsewhere"
	putRef(t, elsewhere, commits[0])
	theirs := fork(t, s, elsewhere, "theirs")
	putRemote(t, "origin", RefForXIDR(issue.ID), theirs)

	if _, err := s.ApplyPush("origin", p, false); err == nil {
		t.Fatal("a push against a remote that had moved was accepted")
	}
	out, err := exec.Command("git", "ls-remote", "origin", RefForXIDR(issue.ID)).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), theirs) {
		t.Errorf("the remote is at %q, and the refused push should have left it at %s",
			strings.TrimSpace(string(out)), shortSHA(theirs))
	}
}

// TestSyncNotes_UnionsBothWays: two clones that linked different commits each
// hold notes the other does not, and neither is a conflict. The reverse index of
// the older layout is folded in the same way and then cleared from the remote,
// so a link made by an older client is not dropped by the next push.
func TestSyncNotes_UnionsBothWays(t *testing.T) {
	gitInitWithOrigin(t)
	s := NewGitStoreWithOutput(&strings.Builder{})
	if out, err := exec.Command("git", "commit", "-q", "--allow-empty", "-m", "root").CombinedOutput(); err != nil {
		t.Fatalf("commit: %v: %s", err, out)
	}
	head, err := s.VerifyCommit("HEAD")
	if err != nil {
		t.Fatal(err)
	}

	// What the remote holds, in both layouts, and nothing here yet.
	if err := s.AddNote(head, "theirs-current"); err != nil {
		t.Fatal(err)
	}
	putRemote(t, "origin", NotesRef, refAtOrEmpty(t, NotesRef))
	dropRef(t, NotesRef)
	if out, err := exec.Command("git", "notes", "--ref="+Gen0NotesRef, "add", "-m", "theirs-older", head).CombinedOutput(); err != nil {
		t.Fatalf("notes add: %v: %s", err, out)
	}
	putRemote(t, "origin", Gen0NotesRef, refAtOrEmpty(t, Gen0NotesRef))
	dropRef(t, Gen0NotesRef)

	// And what this clone has linked in the meantime.
	if err := s.AddNote(head, "ours"); err != nil {
		t.Fatal(err)
	}

	if _, err := s.FetchTracking("origin"); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if err := s.SyncNotes("origin", true); err != nil {
		t.Fatalf("sync notes: %v", err)
	}

	note, err := s.GetNotes(head)
	if err != nil {
		t.Fatalf("the reverse index does not answer: %v", err)
	}
	for _, want := range []string{"ours", "theirs-current", "theirs-older"} {
		if !strings.Contains(note, want) {
			t.Errorf("the merged index does not hold %q: %q", want, note)
		}
	}
	got := remoteRefsOf(t, "origin")
	if !equalStrings(got, []string{NotesRef}) {
		t.Errorf("the remote holds %v, want only %s", got, NotesRef)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
