package commands

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/git-issue/ops"
)

// TestComment_TheCommandItself: `git issue comment <id> text` through the
// command, not through the store. The command resolved the id to a ref for the
// editor's context and then handed that ref to ops.Comment, which resolves ids
// and not refs, so every comment from the command line was refused with "issue
// not found: refs/..." -- and no test called the command (77n1a2gxh12ksjd8pxn0).
// The store helper the sync tests use never went through it.
func TestComment_TheCommandItself(t *testing.T) {
	store := testRepo(t)
	issue, err := ops.Create(store, "Commented", "b")
	if err != nil {
		t.Fatal(err)
	}
	cc, out := sayCC()
	if err := CommentCommand(store).Run(cc, []string{issue.ID[:5], "from", "the", "command"}); err != nil {
		t.Fatalf("comment: %v", err)
	}
	if !strings.Contains(out.String(), "Added comment to issue "+issue.ID) {
		t.Errorf("said %q", out.String())
	}
	sh, err := ops.Show(store, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sh.Comments) != 1 || strings.TrimSpace(sh.Comments[0].Text) != "from the command" {
		t.Errorf("the comment reads as %+v", sh.Comments)
	}

	// --edit, after the id as the usage puts it, changes that comment in place.
	name := strings.TrimPrefix(sh.Comments[0].Path, "discussion/")
	// The text after it is taken as given, a leading dash included.
	for _, args := range [][]string{{"--edit", name[:10], "- edited", "here"}, {"-e=" + name, "- edited", "again"}} {
		cc, out = sayCC()
		if err := CommentCommand(store).Run(cc, append([]string{issue.ID[:5]}, args...)); err != nil {
			t.Fatalf("comment %v: %v", args, err)
		}
		if !strings.Contains(out.String(), "Edited comment "+name+" on issue "+issue.ID) {
			t.Errorf("said %q", out.String())
		}
		if sh, _ = ops.Show(store, issue.ID); len(sh.Comments) != 1 || strings.TrimSpace(sh.Comments[0].Text) != "- edited "+args[len(args)-1] {
			t.Errorf("after %v the comment reads as %+v", args, sh.Comments)
		}
	}
}
