package commands

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/git-issue/issuelib"
)

// TestList_OutsideARepositorySaysSo: "No issues found" outside a repository is
// true and misleading -- there is no repository to have issues
// (82tmk1ywh12ksg5dpxn0).
func TestList_OutsideARepositorySaysSo(t *testing.T) {
	t.Chdir(t.TempDir())
	store := issuelib.NewGitStoreWithOutput(&strings.Builder{})
	cc, out := sayCC()
	err := ListCommand(store).Run(cc, nil)
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("outside a repository, list answered %q, %v", out.String(), err)
	}
}

// TestPush_NoIdIsEveryIssue: `git issue push --dry-run` is the first thing
// anyone types, and it wanted an id or --all (82tmk1ywh12ksg5dpxn0).
func TestPush_NoIdIsEveryIssue(t *testing.T) {
	store, _ := pushTestRepo(t)
	if _, err := store.Create("One", "# One\n\nbody\n"); err != nil {
		t.Fatal(err)
	}
	cc, out := sayCC()
	if err := newPushConfig(store).run(cc, []string{"--dry-run"}); err != nil {
		t.Fatalf("push --dry-run with no id: %v", err)
	}
	if !strings.Contains(out.String(), "Pushing all issues") || !strings.Contains(out.String(), "send to the remote") {
		t.Errorf("said %q", out.String())
	}
}
