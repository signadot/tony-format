package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

// bareOrigin gives a repository a bare origin, and answers the origin's path.
func bareOrigin(t *testing.T, dir string) string {
	t.Helper()
	origin := t.TempDir()
	run(t, origin, "init", "-q", "--bare")
	run(t, dir, "remote", "add", "origin", origin)
	return origin
}

// TestMCP_TheUmbrellaScenario is yg3ck1aah12ksx4ypxn0's definition of done, run
// as one test. A host starts the server from a home directory, nowhere in
// particular, and it serves the working set from ~/.config/git-issue.tony; the
// agent adds verse to the set, files an issue there related to one in
// tony-format, and that relation resolves from a fresh clone of verse alone;
// it edits the verse issue and closes it with the commit that made the change,
// which issue_for_commit finds; a host subscribed to the issue hears a comment
// made from a shell within the poll; and a push sends the verse issue and its
// ext reference to verse's origin, and nothing to tony-format's.
func TestMCP_TheUmbrellaScenario(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	tony, verse := repoDir(t, "tony-format"), repoDir(t, "verse")
	tonyOrigin, verseOrigin := bareOrigin(t, tony), bareOrigin(t, verse)
	tonyStore := issuelib.NewGitStoreAt(tony, &strings.Builder{})
	inTony, err := ops.Create(tonyStore, "In tony-format", "The design.")
	if err != nil {
		t.Fatal(err)
	}
	if err := persistWorkingSet(tony); err != nil {
		t.Fatal(err)
	}

	// Started from a home directory: nowhere in particular.
	t.Chdir(home)
	ws, err := startingSet(nil, &strings.Builder{})
	if err != nil {
		t.Fatal(err)
	}
	if got := ws.list(); len(got) != 1 || got[0].Name != "tony-format" || got[0].From != "config" {
		t.Fatalf("the starting set is %+v", got)
	}
	cs, n := mcpClientWatching(t, ws, 50*time.Millisecond)
	ctx := context.Background()

	// The agent adds verse, files an issue there, and relates it to tony-format's.
	var repos struct{ Repos []struct{ Name string } }
	call(t, cs, "repo_add", map[string]any{"path": verse, "persist": true}, &repos)
	if len(repos.Repos) != 2 {
		t.Fatalf("after repo_add: %+v", repos)
	}
	if raw, _ := os.ReadFile(filepath.Join(home, ".config", "git-issue.tony")); !strings.Contains(string(raw), verse) {
		t.Errorf("persist did not record verse: %q", raw)
	}
	var created struct{ ID, Repo string }
	call(t, cs, "issue_create", map[string]any{"repo": "verse", "title": "In verse", "body": "The work."}, &created)
	if created.Repo != "verse" {
		t.Fatalf("created in %s", created.Repo)
	}
	var rel struct {
		Changed  bool
		Mirrored string
	}
	call(t, cs, "issue_relate", map[string]any{"id": created.ID, "other": inTony.ID, "kind": "related"}, &rel)
	if !rel.Changed || rel.Mirrored != "tony-format" {
		t.Fatalf("relate across: %+v", rel)
	}

	// The relation resolves from a fresh clone of verse alone.
	var report struct{ Whole bool }
	call(t, cs, "issue_push", map[string]any{"repo": "verse"}, &report)
	if !report.Whole {
		t.Fatal("the push was not whole")
	}
	clone := secondClone(t, verseOrigin)
	cloneStore := issuelib.NewGitStoreAt(clone, &strings.Builder{})
	if r, err := ops.Pull(cloneStore, "origin", false, false); err != nil || r.Err() != nil {
		t.Fatalf("clone pull: %v, %v", err, r.Err())
	}
	sh, err := ops.Show(cloneStore, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sh.Related) != 1 || sh.Related[0].ID != inTony.ID || sh.Related[0].Title != "In tony-format" || sh.Related[0].Err != "" {
		t.Errorf("from the clone alone the relation reads as %+v", sh.Related)
	}
	if refs := run(t, tonyOrigin, "for-each-ref", "--format=%(refname)", "refs/"); strings.TrimSpace(refs) != "" {
		t.Errorf("tony-format's origin received %q", refs)
	}

	// Edit, then close with the commit that made the change; the commit finds it.
	call(t, cs, "issue_edit", map[string]any{"id": created.ID, "body": "The work, done."}, nil)
	run(t, verse, "commit", "-q", "--allow-empty", "-m", "the change\n\nIssue: "+created.ID)
	sha := strings.TrimSpace(run(t, verse, "rev-parse", "HEAD"))
	var status struct {
		Status   string
		ClosedBy string `json:"closed_by"`
	}
	call(t, cs, "issue_close", map[string]any{"id": created.ID, "commit": sha}, &status)
	if status.Status != "closed" || status.ClosedBy != sha {
		t.Errorf("close answered %+v", status)
	}
	call(t, cs, "issue_link", map[string]any{"id": created.ID, "commit": sha}, nil)
	var forCommit struct{ Issues []struct{ ID string } }
	call(t, cs, "issue_for_commit", map[string]any{"repo": "verse", "commit": sha}, &forCommit)
	if len(forCommit.Issues) != 1 || forCommit.Issues[0].ID != created.ID {
		t.Errorf("for-commit: %+v", forCommit)
	}

	// A subscribed host hears a comment made from a shell.
	if err := cs.Subscribe(ctx, &mcp.SubscribeParams{URI: uriFor(created.ID)}); err != nil {
		t.Fatal(err)
	}
	drain(n)
	verseStore := issuelib.NewGitStoreAt(verse, &strings.Builder{})
	if _, _, err := ops.Comment(verseStore, created.ID, "from a shell"); err != nil {
		t.Fatal(err)
	}
	hearsUpdate(t, n, uriFor(created.ID), 2*time.Second)

	// The push sends the verse issue and its ext reference to verse's origin,
	// and nothing to tony-format's.
	call(t, cs, "issue_push", map[string]any{"repo": "verse"}, &report)
	if !report.Whole {
		t.Fatal("the second push was not whole")
	}
	got := run(t, verseOrigin, "for-each-ref", "--format=%(refname)", "refs/git-issues/")
	for _, want := range []string{
		issuelib.ClosedRefForXIDR(created.ID),
		issuelib.ExtRefForXIDR("tony-format", inTony.ID),
		issuelib.SourceRef("tony-format"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("verse's origin lacks %s:\n%s", want, got)
		}
	}
	if refs := run(t, tonyOrigin, "for-each-ref", "--format=%(refname)", "refs/"); strings.TrimSpace(refs) != "" {
		t.Errorf("tony-format's origin received %q", refs)
	}
}
