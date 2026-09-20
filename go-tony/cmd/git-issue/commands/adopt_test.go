package commands

import (
	"sort"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

// demoteToGen0 puts the issue's ref where a git-issue older than this generation
// kept it, and answers the commit it is at: a clone that binary had written.
func demoteToGen0(t *testing.T, store issuelib.Store, id string) string {
	t.Helper()
	ref, err := store.FindRef(id)
	if err != nil {
		t.Fatalf("find %s: %v", id, err)
	}
	commit, err := store.GetRefCommit(ref)
	if err != nil {
		t.Fatalf("read %s: %v", ref, err)
	}
	gen0 := issuelib.Gen0RefForXIDR(id)
	if issuelib.IsClosedRef(ref) {
		gen0 = issuelib.Gen0ClosedRefForXIDR(id)
	}
	run(t, "", "update-ref", gen0, commit)
	run(t, "", "update-ref", "-d", ref)
	return commit
}

// TestAdopt_ACloneAnOlderBinaryWroteIsUsable: a repository whose issues are all
// where the previous layout put them lists, shows and edits them, because every
// read adopts first. Without that, upgrading the binary would look like losing
// every issue.
func TestAdopt_ACloneAnOlderBinaryWroteIsUsable(t *testing.T) {
	store := testRepo(t)
	issue, err := store.Create("Older", "# Older\n\nbody\n")
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	closed, err := store.Create("Older, closed", "# Older, closed\n\nbody\n")
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	moveIssue(t, store, closed.ID, "closed")
	openAt := demoteToGen0(t, store, issue.ID)
	demoteToGen0(t, store, closed.ID)

	// A binary starting fresh in that clone.
	fresh := issuelib.NewGitStoreWithOutput(&strings.Builder{})
	issues, err := fresh.List(true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("a clone of two issues listed %d", len(issues))
	}
	got, _, err := fresh.Get(issue.ID)
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if got.Title != "Older" {
		t.Errorf("title %q, want %q", got.Title, "Older")
	}
	if got.Ref != issuelib.RefForXIDR(issue.ID) {
		t.Errorf("the issue is at %q, want %q", got.Ref, issuelib.RefForXIDR(issue.ID))
	}
	if at, err := fresh.GetRefCommit(got.Ref); err != nil || at != openAt {
		t.Errorf("the adopted ref is at %q, want the commit it was already at, %q", at, openAt)
	}

	// And it edits: the chain carries on from where the older binary left it.
	if err := fresh.Update(got, "comment", map[string]string{"discussion/a.md": "a\n"}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if at, _ := fresh.GetRefCommit(got.Ref); at == openAt {
		t.Error("an edit after adoption did not move the ref")
	}
}

// TestPull_AdoptsAnUnmigratedRemote: a remote nothing of this generation has
// pushed to keeps its issues where the older layout put them. A pull fetches
// them and adopts them, and adopts them even when this store has already read --
// the implicit adoption a read takes is spent by then, and what arrived after it
// would sit unadopted and unseen. Nothing is written to the remote: a pull does
// not migrate it, a push does.
func TestPull_AdoptsAnUnmigratedRemote(t *testing.T) {
	store, origin := pushTestRepo(t)
	one, err := store.Create("Theirs one", "# Theirs one\n\nbody\n")
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	two, err := store.Create("Theirs two", "# Theirs two\n\nbody\n")
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	moveIssue(t, store, two.ID, "closed")

	// Put them on origin the way the older binary would have, and forget them here.
	for _, id := range []string{one.ID, two.ID} {
		ref, err := store.FindRef(id)
		if err != nil {
			t.Fatal(err)
		}
		commit, err := store.GetRefCommit(ref)
		if err != nil {
			t.Fatal(err)
		}
		gen0 := issuelib.Gen0RefForXIDR(id)
		if issuelib.IsClosedRef(ref) {
			gen0 = issuelib.Gen0ClosedRefForXIDR(id)
		}
		run(t, "", "push", origin, commit+":"+gen0)
		run(t, "", "update-ref", "-d", ref)
	}

	// Spend the store's implicit adoption before any of it arrives.
	if refs, err := store.ListRefs(true); err != nil || len(refs) != 0 {
		t.Fatalf("before the pull the clone holds %v, %v", refs, err)
	}

	pull := newPullConfig(store)
	if err := pull.run(pushCC(), []string{"origin"}); err != nil {
		t.Fatalf("pull: %v", err)
	}

	issues, err := store.List(true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("after pulling two issues the clone lists %d", len(issues))
	}
	var titles []string
	for _, issue := range issues {
		titles = append(titles, issue.Title)
		if issuelib.IsGen0Ref(issue.Ref) {
			t.Errorf("%s was left in the older layout, at %s", issue.Title, issue.Ref)
		}
	}
	sort.Strings(titles)
	if want := []string{"Theirs one", "Theirs two"}; !equal(titles, want) {
		t.Errorf("the clone lists %v, want %v", titles, want)
	}
	if got := remoteIssueRefs(t, origin); len(got) != 0 {
		t.Errorf("the pull wrote %v to the remote; only a push migrates one", got)
	}
	if got := remoteRefs(t, origin, issuelib.Gen0OpenPrefix+"*", issuelib.Gen0ClosedPrefix+"*"); len(got) != 2 {
		t.Errorf("the remote's own refs were disturbed: %v", got)
	}
}
