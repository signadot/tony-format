package commands

import (
	"os"
	"strings"
	"testing"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

// secondClone makes another working repository sharing origin, and answers its
// path: the other person, in the only arrangement where a sync can lose
// anything.
func secondClone(t *testing.T, origin string) string {
	t.Helper()
	dir := t.TempDir()
	run(t, "", "init", "-q", dir)
	run(t, dir, "config", "user.email", "other@example.com")
	run(t, dir, "config", "user.name", "Other")
	run(t, dir, "remote", "add", "origin", origin)
	return dir
}

// inClone runs fn with dir as the repository the git binary finds, and a store
// of its own. A GitStore holds no path, so which clone it acts on is the
// process's working directory -- which is also why nothing here may run in
// parallel.
func inClone(t *testing.T, dir string, fn func(store issuelib.Store)) {
	t.Helper()
	was, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	defer func() {
		if err := os.Chdir(was); err != nil {
			t.Fatalf("chdir back: %v", err)
		}
	}()
	fn(issuelib.NewGitStoreWithOutput(&strings.Builder{}))
}

// sayCC is a context whose output a test can read.
func sayCC() (*cli.Context, *strings.Builder) {
	var out strings.Builder
	return &cli.Context{Out: nopWriteCloser{&out}, Err: nopWriteCloser{&strings.Builder{}}}, &out
}

// comment adds one to an issue, as the comment command does.
func comment(t *testing.T, store issuelib.Store, id, name string) string {
	t.Helper()
	issue, _, err := store.Get(id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	if err := store.Update(issue, "comment: "+name,
		map[string]string{"discussion/" + name + ".md": name + "\n"}); err != nil {
		t.Fatalf("comment on %s: %v", id, err)
	}
	at, err := store.GetRefCommit(issue.Ref)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

// TestSync_BringsTogetherWorkDoneInTwoClones is the loss this whole change is
// about. Two clones comment on one issue and the first pushes; the second used
// to have its comment reset by the pull, or to overwrite the first's by pushing.
// Neither chain carries the other, and neither has to be chosen: they are merged,
// both comments survive, and the merge has both tips as parents, so the first
// clone fast-forwards to it without being told it lost.
func TestSync_BringsTogetherWorkDoneInTwoClones(t *testing.T) {
	storeA, origin := pushTestRepo(t)
	issue, err := storeA.Create("Shared", "# Shared\n\nbody\n")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pushA := newPushConfig(storeA)
	if err := pushA.pushAll(pushCC(), "origin"); err != nil {
		t.Fatalf("push: %v", err)
	}

	b := secondClone(t, origin)
	inClone(t, b, func(storeB issuelib.Store) {
		if err := newPullConfig(storeB).run(pushCC(), []string{"origin"}); err != nil {
			t.Fatalf("B pull: %v", err)
		}
		comment(t, storeB, issue.ID, "from-b")
	})
	comment(t, storeA, issue.ID, "from-a")
	if err := pushA.pushAll(pushCC(), "origin"); err != nil {
		t.Fatalf("A push: %v", err)
	}

	inClone(t, b, func(storeB issuelib.Store) {
		if err := newPullConfig(storeB).run(pushCC(), []string{"origin"}); err != nil {
			t.Fatalf("B pull after A pushed: %v", err)
		}
		files, err := storeB.ListDir(issuelib.RefForXIDR(issue.ID), "discussion")
		if err != nil {
			t.Fatalf("B read the discussion: %v", err)
		}
		for _, want := range []string{"from-a.md", "from-b.md"} {
			if _, ok := files[want]; !ok {
				t.Errorf("B holds %v, and %s is missing", files, want)
			}
		}
		if err := newPushConfig(storeB).pushAll(pushCC(), "origin"); err != nil {
			t.Fatalf("B push: %v", err)
		}
	})

	if err := newPullConfig(storeA).run(pushCC(), []string{"origin"}); err != nil {
		t.Fatalf("A pull: %v", err)
	}
	files, err := storeA.ListDir(issuelib.RefForXIDR(issue.ID), "discussion")
	if err != nil {
		t.Fatalf("A read the discussion: %v", err)
	}
	for _, want := range []string{"from-a.md", "from-b.md"} {
		if _, ok := files[want]; !ok {
			t.Errorf("A holds %v, and %s is missing", files, want)
		}
	}
}

// TestSync_RefusesWhatItCannotBringTogether: two people rewrote the same
// description. There is no rule for that better than asking them, so the issue
// is named with the path and left alone, and the command exits non-zero;
// --force is how one of them decides it.
func TestSync_RefusesWhatItCannotBringTogether(t *testing.T) {
	storeA, origin := pushTestRepo(t)
	issue, err := storeA.Create("Contested", "# Contested\n\nbody\n")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pushA := newPushConfig(storeA)
	if err := pushA.pushAll(pushCC(), "origin"); err != nil {
		t.Fatalf("push: %v", err)
	}

	rewrite := func(store issuelib.Store, body string) {
		t.Helper()
		got, _, err := store.Get(issue.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if err := store.Update(got, "rewrite", map[string]string{"description.md": body}); err != nil {
			t.Fatalf("rewrite: %v", err)
		}
	}

	b := secondClone(t, origin)
	var theirs string
	inClone(t, b, func(storeB issuelib.Store) {
		if err := newPullConfig(storeB).run(pushCC(), []string{"origin"}); err != nil {
			t.Fatalf("B pull: %v", err)
		}
		rewrite(storeB, "# Contested\n\nas B sees it\n")
		theirs, _ = storeB.GetRefCommit(issuelib.RefForXIDR(issue.ID))
	})
	rewrite(storeA, "# Contested\n\nas A sees it\n")
	ours, _ := storeA.GetRefCommit(issuelib.RefForXIDR(issue.ID))
	if err := pushA.pushAll(pushCC(), "origin"); err != nil {
		t.Fatalf("A push: %v", err)
	}

	inClone(t, b, func(storeB issuelib.Store) {
		cc, out := sayCC()
		if err := newPullConfig(storeB).run(cc, []string{"origin"}); err == nil {
			t.Error("a description rewritten on both sides was answered as a success")
		}
		for _, want := range []string{issue.ID, "description.md", "--force"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("the refusal does not mention %q: %q", want, out.String())
			}
		}
		if at, _ := storeB.GetRefCommit(issuelib.RefForXIDR(issue.ID)); at != theirs {
			t.Errorf("the refused pull moved this clone to %s", shortSHA(at))
		}

		if err := newPullConfig(storeB).run(pushCC(), []string{"--force", "origin"}); err != nil {
			t.Fatalf("forced pull: %v", err)
		}
		if at, _ := storeB.GetRefCommit(issuelib.RefForXIDR(issue.ID)); at != ours {
			t.Errorf("the forced pull left this clone at %s, want %s", shortSHA(at), shortSHA(ours))
		}
		if logged := run(t, b, "reflog", "show", "--format=%H", issuelib.RefForXIDR(issue.ID)); !strings.Contains(logged, theirs) {
			t.Errorf("what the force overwrote is not in the reflog: %q", logged)
		}
	})
}

// TestPush_KeepsWorkTheRemoteHasAndThisCloneDoesNot: closing an issue moves its
// ref, and a push mirrors the move by deleting the ref it came from. That
// deletion used to be unconditional, so a close here silently deleted whatever
// someone else had pushed to the open ref, and the comments on it. The remote's
// tip is part of the decision now: the close and the other clone's comment are
// brought together, and what the remote ends up holding carries both.
func TestPush_KeepsWorkTheRemoteHasAndThisCloneDoesNot(t *testing.T) {
	storeA, origin := pushTestRepo(t)
	issue, err := storeA.Create("Contested", "# Contested\n\nbody\n")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pushA := newPushConfig(storeA)
	if err := pushA.pushAll(pushCC(), "origin"); err != nil {
		t.Fatalf("push: %v", err)
	}

	b := secondClone(t, origin)
	inClone(t, b, func(storeB issuelib.Store) {
		if err := newPullConfig(storeB).run(pushCC(), []string{"origin"}); err != nil {
			t.Fatalf("B pull: %v", err)
		}
		comment(t, storeB, issue.ID, "still-a-problem")
		if err := newPushConfig(storeB).pushAll(pushCC(), "origin"); err != nil {
			t.Fatalf("B push: %v", err)
		}
	})

	// Meanwhile this clone closed it, which moves the ref.
	moveIssue(t, storeA, issue.ID, "closed")
	if err := pushA.pushAll(pushCC(), "origin"); err != nil {
		t.Fatalf("A push: %v", err)
	}

	// One ref, the closed one, and the other clone's comment is in it.
	want := []string{issuelib.ClosedRefForXIDR(issue.ID)}
	if got := remoteRefs(t, origin, "refs/git-issues/"); !equal(got, want) {
		t.Fatalf("the remote holds %v, want %v", got, want)
	}
	at := strings.TrimSpace(run(t, origin, "rev-parse", want[0]))
	tree := run(t, origin, "ls-tree", "-r", "--name-only", at)
	if !strings.Contains(tree, "still-a-problem.md") {
		t.Errorf("the comment made in the other clone is gone: %q", tree)
	}
	if issue, _, err := storeA.Get(issue.ID); err != nil || issue.Status != "closed" {
		t.Errorf("the close did not survive the merge: %+v, %v", issue, err)
	}
}

// TestPush_MigratesARemoteAndTripsOnAnOlderClient: a remote holding its issues
// where the older layout put them is migrated by an ordinary push, with no
// command of its own and no commit rewritten. After that the older namespace is
// empty, so anything appearing in it was pushed by a client too old to see the
// current refs -- which is reported, and cleared by the next push, with the work
// kept.
func TestPush_MigratesARemoteAndTripsOnAnOlderClient(t *testing.T) {
	storeA, origin := pushTestRepo(t)
	issue, err := storeA.Create("From before", "# From before\n\nbody\n")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	at, err := storeA.GetRefCommit(issuelib.RefForXIDR(issue.ID))
	if err != nil {
		t.Fatal(err)
	}
	// The remote as an older git-issue left it.
	run(t, "", "push", origin, at+":"+issuelib.Gen0RefForXIDR(issue.ID))
	run(t, "", "update-ref", "-d", issuelib.RefForXIDR(issue.ID))

	pull := newPullConfig(storeA)
	if err := pull.run(pushCC(), []string{"origin"}); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if issues, _ := storeA.List(true); len(issues) != 1 {
		t.Fatalf("the clone lists %d issues, want 1", len(issues))
	}

	push := newPushConfig(storeA)
	if err := push.pushAll(pushCC(), "origin"); err != nil {
		t.Fatalf("push: %v", err)
	}
	want := []string{issuelib.RefForXIDR(issue.ID)}
	if got := remoteRefs(t, origin, "refs/"); !equal(got, want) {
		t.Fatalf("after migrating, the remote holds %v, want %v", got, want)
	}
	if now := strings.TrimSpace(run(t, origin, "rev-parse", want[0])); now != at {
		t.Errorf("the migrated ref is at %s, want the commit it was already at, %s",
			shortSHA(now), shortSHA(at))
	}

	// Someone still running the older binary pushes to where it knows.
	newer := comment(t, storeA, issue.ID, "from-the-old-binary")
	run(t, "", "push", origin, newer+":"+issuelib.Gen0RefForXIDR(issue.ID))
	run(t, "", "update-ref", issuelib.RefForXIDR(issue.ID), at)

	cc, out := sayCC()
	if err := pull.run(cc, []string{"origin"}); err != nil {
		t.Fatalf("pull after the older client: %v", err)
	}
	if !strings.Contains(out.String(), "older than this one") {
		t.Errorf("the older client was not reported: %q", out.String())
	}
	if got, _ := storeA.GetRefCommit(issuelib.RefForXIDR(issue.ID)); got != newer {
		t.Errorf("their work was not taken: the clone is at %s, want %s", shortSHA(got), shortSHA(newer))
	}

	if err := push.pushAll(pushCC(), "origin"); err != nil {
		t.Fatalf("push after the older client: %v", err)
	}
	if got := remoteRefs(t, origin, "refs/"); !equal(got, want) {
		t.Errorf("the older namespace was not cleared: the remote holds %v", got)
	}
}

// TestSync_NotesReachBothClones: the reverse index is one ref for the whole
// repository, so force-pushing it replaced whatever the remote had. Two clones
// that linked different commits both keep their links, in both directions.
func TestSync_NotesReachBothClones(t *testing.T) {
	storeA, origin := pushTestRepo(t)
	mine, err := storeA.Create("Mine", "# Mine\n\nbody\n")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	theirs, err := storeA.Create("Theirs", "# Theirs\n\nbody\n")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	run(t, "", "commit", "-q", "--allow-empty", "-m", "one")
	first := strings.TrimSpace(run(t, "", "rev-parse", "HEAD"))
	run(t, "", "commit", "-q", "--allow-empty", "-m", "two")
	second := strings.TrimSpace(run(t, "", "rev-parse", "HEAD"))

	if err := storeA.AddNote(first, mine.ID); err != nil {
		t.Fatal(err)
	}
	pushA := newPushConfig(storeA)
	if err := pushA.pushAll(pushCC(), "origin"); err != nil {
		t.Fatalf("A push: %v", err)
	}

	// The commits themselves have to reach the other clone the ordinary way: an
	// issue links a commit, and does not carry it.
	run(t, "", "push", "-q", origin, second+":refs/heads/main")
	b := secondClone(t, origin)
	run(t, b, "fetch", "-q", "origin", "refs/heads/main:refs/heads/fetched")
	inClone(t, b, func(storeB issuelib.Store) {
		if err := newPullConfig(storeB).run(pushCC(), []string{"origin"}); err != nil {
			t.Fatalf("B pull: %v", err)
		}
		if err := storeB.AddNote(second, theirs.ID); err != nil {
			t.Fatal(err)
		}
		if err := newPushConfig(storeB).pushAll(pushCC(), "origin"); err != nil {
			t.Fatalf("B push: %v", err)
		}
		for commit, want := range map[string]string{first: mine.ID, second: theirs.ID} {
			if got, err := storeB.GetNotes(commit); err != nil || !strings.Contains(got, want) {
				t.Errorf("B: %s is not linked to %s: %q, %v", shortSHA(commit), want, got, err)
			}
		}
	})

	if err := newPullConfig(storeA).run(pushCC(), []string{"origin"}); err != nil {
		t.Fatalf("A pull: %v", err)
	}
	for commit, want := range map[string]string{first: mine.ID, second: theirs.ID} {
		if got, err := storeA.GetNotes(commit); err != nil || !strings.Contains(got, want) {
			t.Errorf("A: %s is not linked to %s: %q, %v", shortSHA(commit), want, got, err)
		}
	}
}

// TestSync_DryRunWritesNothing: a run that says what it would do must not do any
// of it, on either side.
func TestSync_DryRunWritesNothing(t *testing.T) {
	storeA, origin := pushTestRepo(t)
	issue, err := storeA.Create("Planned", "# Planned\n\nbody\n")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cc, out := sayCC()
	if err := newPushConfig(storeA).run(cc, []string{"--all", "--dry-run"}); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !strings.Contains(out.String(), issue.ID) {
		t.Errorf("the dry run does not name the issue it would send: %q", out.String())
	}
	if got := remoteIssueRefs(t, origin); len(got) != 0 {
		t.Errorf("the dry run pushed %v", got)
	}
}
