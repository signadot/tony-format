package ops

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/signadot/tony-format/git-issue/issuelib"
)

// GitStore drives the git binary in the process's working directory, so a test
// repo is a temp dir plus t.Chdir, and these tests are serial by nature.
func testRepo(t *testing.T) issuelib.Store {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"commit", "-q", "--allow-empty", "-m", "root"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return issuelib.NewGitStoreWithOutput(&strings.Builder{})
}

// TestAnIssuesLife runs one issue through every operation, as data: what each
// answers is what the front ends show, so this is what both of them rest on.
func TestAnIssuesLife(t *testing.T) {
	s := testRepo(t)

	if _, err := Create(s, "Untitled", "  "); err == nil {
		t.Error("an empty body was accepted")
	}
	issue, err := Create(s, "A thing to do", "The body.\n")
	if err != nil {
		t.Fatal(err)
	}
	sh, err := Show(s, issue.ID[:4])
	if err != nil {
		t.Fatalf("show by prefix: %v", err)
	}
	if sh.Title != "A thing to do" || sh.Body != "The body." || sh.Status != "open" {
		t.Errorf("shown as %q / %q [%s]", sh.Title, sh.Body, sh.Status)
	}

	if _, path, err := Comment(s, issue.ID, "first\nmore"); err != nil || !strings.HasPrefix(path, "discussion/") {
		t.Fatalf("comment: %v at %q", err, path)
	}
	if _, _, err := Comment(s, issue.ID, " \n"); err == nil {
		t.Error("an empty comment was accepted")
	}
	if _, path, err := Comment(s, issue.ID, "second"); err != nil || !strings.HasPrefix(path, "discussion/") {
		t.Fatalf("comment: %v at %q", err, path)
	}
	// Two comments in one second order by path, so the texts are checked as a set.
	sh, _ = Show(s, issue.ID)
	var texts []string
	for _, c := range sh.Comments {
		if !c.HasTime || !strings.HasPrefix(c.Content, "<!-- ") {
			t.Errorf("comment %s has no header: %q", c.Path, c.Content)
		}
		texts = append(texts, c.Text)
	}
	if len(texts) != 2 || !(texts[0] == "first\nmore\n" || texts[1] == "first\nmore\n") {
		t.Errorf("comments read as %q", texts)
	}

	labelled, err := Label(s, issue.ID, []string{"Bug", "severity=high"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(labelled.Labels, ","); got != "bug,severity=high" {
		t.Errorf("labels %q", got)
	}
	labelled, err = Label(s, issue.ID, []string{"severity=low"}, []string{"bug"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(labelled.Labels, ","); got != "severity=low" {
		t.Errorf("after replacing a key and removing a label: %q", got)
	}
	if _, err := Label(s, issue.ID, []string{"=x"}, nil); err == nil || !strings.Contains(err.Error(), "no key") {
		t.Errorf("a label with no key was accepted: %v", err)
	}
	if _, err := Label(s, issue.ID, nil, nil); err == nil {
		t.Error("nothing to do was accepted")
	}

	listed, err := List(s, false, "severity=low")
	if err != nil || len(listed) != 1 || listed[0].ID != issue.ID {
		t.Errorf("list by label: %v, %d issues", err, len(listed))
	}

	head := strings.TrimSpace(gitOut(t, "rev-parse", "HEAD"))
	if _, sha, err := Link(s, issue.ID, "HEAD"); err != nil || sha != head {
		t.Fatalf("link: %v, %q", err, sha)
	}
	if _, linked, err := ForCommit(s, head); err != nil || len(linked) != 1 || linked[0].ID != issue.ID {
		t.Errorf("for-commit: %v, %+v", err, linked)
	}

	other, err := Create(s, "Another", "body")
	if err != nil {
		t.Fatal(err)
	}
	from, to, changed, err := Relate(s, issue.ID, other.ID, Blocks)
	if err != nil || !changed || from.ID != issue.ID || to.ID != other.ID {
		t.Fatalf("relate: %v changed=%v", err, changed)
	}
	if _, _, changed, err := Relate(s, issue.ID, other.ID, Blocks); err != nil || changed {
		t.Errorf("relating again: %v changed=%v", err, changed)
	}
	if _, _, _, err := Relate(s, issue.ID, other.ID, Relation("owns")); err == nil {
		t.Error("an unknown relation was accepted")
	}
	sh, _ = Show(s, other.ID)
	if len(sh.BlockedBy) != 1 || sh.BlockedBy[0].ID != issue.ID || sh.BlockedBy[0].Title != "A thing to do" {
		t.Errorf("the other side reads blocked_by as %+v", sh.BlockedBy)
	}

	closed, err := Close(s, issue.ID, "HEAD")
	if err != nil || closed.ClosedBy == nil || *closed.ClosedBy != head {
		t.Fatalf("close: %v, closed by %v", err, closed.ClosedBy)
	}
	if _, err := Close(s, issue.ID, ""); err == nil {
		t.Error("closing a closed issue was accepted")
	}
	if sh, _ = Show(s, issue.ID); sh.Status != "closed" {
		t.Errorf("after close: %s", sh.Status)
	}
	if listed, _ := List(s, false, ""); len(listed) != 1 {
		t.Errorf("open list after close has %d", len(listed))
	}
	if listed, _ := List(s, true, ""); len(listed) != 2 {
		t.Errorf("full list after close has %d", len(listed))
	}
	if _, err := Reopen(s, issue.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := Reopen(s, issue.ID); err == nil {
		t.Error("reopening an open issue was accepted")
	}
	if sh, _ = Show(s, issue.ID); sh.Status != "open" || sh.Issue.ClosedBy != nil {
		t.Errorf("after reopen: %s, closed by %v", sh.Status, sh.Issue.ClosedBy)
	}
}

func gitOut(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}
