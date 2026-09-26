package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/signadot/tony-format/git-issue/issuelib"
)

// repoDir makes a repository named name under a temp directory, without
// chdir, and answers its path.
func repoDir(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "init", "-q", "-b", "main")
	run(t, dir, "config", "user.email", "test@example.com")
	run(t, dir, "config", "user.name", "Test")
	run(t, dir, "commit", "-q", "--allow-empty", "-m", "root")
	return dir
}

// mcpClientFor connects a client to a server over a working set.
func mcpClientFor(t *testing.T, ws *workspace) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	server := MCPServerFor(ws)
	go func() {
		if err := server.Run(ctx, serverT); err != nil {
			t.Logf("server: %v", err)
		}
	}()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// TestMCP_ServesSeveralRepositories: one server over two repositories. An id
// finds its repository; the tools that make, list and sync take repo when more
// than one is served; a relation across the two mirrors the far issue into the
// near repository first, and resolves there alone (7qfhwth7h12ksxcwpxn0).
func TestMCP_ServesSeveralRepositories(t *testing.T) {
	tonyDir, verseDir := repoDir(t, "tony"), repoDir(t, "verse")
	ws, err := startingSet([]string{tonyDir, verseDir}, &strings.Builder{})
	if err != nil {
		t.Fatal(err)
	}
	cs := mcpClientFor(t, ws)

	var repos struct {
		Repos []struct{ Name, Path, From string }
	}
	call(t, cs, "repo_list", map[string]any{}, &repos)
	if len(repos.Repos) != 2 || repos.Repos[0].Name != "tony" || repos.Repos[1].Name != "verse" || repos.Repos[0].From != "-C" {
		t.Fatalf("repo_list: %+v", repos)
	}

	if msg := refused(t, cs, "issue_create", map[string]any{"title": "Where?", "body": "b"}); !strings.Contains(msg, "which repository") {
		t.Errorf("create without repo was refused with %q", msg)
	}
	var inTony, inVerse struct{ ID, Repo string }
	call(t, cs, "issue_create", map[string]any{"repo": "tony", "title": "In tony", "body": "t"}, &inTony)
	call(t, cs, "issue_create", map[string]any{"repo": "verse", "title": "In verse", "body": "v"}, &inVerse)
	if inTony.Repo != "tony" || inVerse.Repo != "verse" {
		t.Errorf("created in %s and %s", inTony.Repo, inVerse.Repo)
	}

	// An id finds its repository, by full id and by prefix.
	var shown struct{ ID, Repo, Title string }
	call(t, cs, "issue_show", map[string]any{"id": inVerse.ID}, &shown)
	if shown.Repo != "verse" || shown.Title != "In verse" {
		t.Errorf("show found %+v", shown)
	}
	call(t, cs, "issue_show", map[string]any{"id": inTony.ID[:6]}, &shown)
	if shown.Repo != "tony" {
		t.Errorf("show by prefix found %+v", shown)
	}
	// A prefix both ids share, if they share one, is ambiguous and names both.
	common := 0
	for common < len(inTony.ID) && inTony.ID[common] == inVerse.ID[common] {
		common++
	}
	if common > 0 {
		msg := refused(t, cs, "issue_show", map[string]any{"id": inTony.ID[:common]})
		if !strings.Contains(msg, "ambiguous") || !strings.Contains(msg, "tony") || !strings.Contains(msg, "verse") {
			t.Errorf("an ambiguous prefix was refused with %q", msg)
		}
	}

	// list: all served, each row saying where; or one.
	var list struct{ Issues []struct{ ID, Repo string } }
	call(t, cs, "issue_list", map[string]any{}, &list)
	if len(list.Issues) != 2 {
		t.Fatalf("list across: %+v", list)
	}
	seen := map[string]string{}
	for _, row := range list.Issues {
		seen[row.ID] = row.Repo
	}
	if seen[inTony.ID] != "tony" || seen[inVerse.ID] != "verse" {
		t.Errorf("rows say %v", seen)
	}
	call(t, cs, "issue_list", map[string]any{"repo": "verse"}, &list)
	if len(list.Issues) != 1 || list.Issues[0].ID != inVerse.ID {
		t.Errorf("list of verse: %+v", list)
	}

	// A relation across: the far issue is mirrored into the near repository.
	var rel struct {
		Changed  bool
		Mirrored string
	}
	call(t, cs, "issue_relate", map[string]any{"id": inVerse.ID, "other": inTony.ID, "kind": "blocks"}, &rel)
	if !rel.Changed || rel.Mirrored != "tony" {
		t.Errorf("relate across: %+v", rel)
	}
	verse := issuelib.NewGitStoreAt(verseDir, &strings.Builder{})
	if ref, err := verse.FindRef(inTony.ID); err != nil || ref != issuelib.ExtRefForXIDR("tony", inTony.ID) {
		t.Errorf("verse holds the mirror at %q, %v", ref, err)
	}
	var verseShown struct {
		Blocks []struct{ ID, Title, Error string }
	}
	call(t, cs, "issue_show", map[string]any{"id": inVerse.ID}, &verseShown)
	if len(verseShown.Blocks) != 1 || verseShown.Blocks[0].Title != "In tony" || verseShown.Blocks[0].Error != "" {
		t.Errorf("the relation reads as %+v", verseShown.Blocks)
	}
	// The id is now in two repositories, its own and the one mirroring it, and
	// its own wins: that is where it is written.
	call(t, cs, "issue_show", map[string]any{"id": inTony.ID}, &shown)
	if shown.Repo != "tony" {
		t.Errorf("the original's repository is %s", shown.Repo)
	}

	// The working set changes while the server runs.
	otherDir := repoDir(t, "other")
	call(t, cs, "repo_add", map[string]any{"path": otherDir}, &repos)
	if len(repos.Repos) != 3 || repos.Repos[2].Name != "other" || repos.Repos[2].From != "repo_add" {
		t.Errorf("after repo_add: %+v", repos)
	}
	call(t, cs, "issue_create", map[string]any{"repo": "other", "title": "Elsewhere", "body": "e"}, &inTony)
	call(t, cs, "repo_remove", map[string]any{"repo": "other"}, &repos)
	if len(repos.Repos) != 2 {
		t.Errorf("after repo_remove: %+v", repos)
	}
	if msg := refused(t, cs, "issue_show", map[string]any{"id": inTony.ID}); !strings.Contains(msg, "not found") {
		t.Errorf("an issue of a removed repository was found: %q", msg)
	}
	if msg := refused(t, cs, "repo_add", map[string]any{"path": t.TempDir()}); !strings.Contains(msg, "not a git repository") {
		t.Errorf("a directory that is not a repository was served: %q", msg)
	}
}

// TestMCP_StartingSet: the set comes from -C, else the config file, else the
// working directory; outside every one of those the server refuses.
func TestMCP_StartingSet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	a, b := repoDir(t, "a"), repoDir(t, "b")

	// Nothing configured, not in a repository: refused.
	t.Chdir(t.TempDir())
	if _, err := startingSet(nil, &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "nothing to serve") {
		t.Errorf("a server with nothing to serve started: %v", err)
	}

	// The config file.
	if err := persistWorkingSet(a); err != nil {
		t.Fatal(err)
	}
	if err := persistWorkingSet(b); err != nil {
		t.Fatal(err)
	}
	if err := persistWorkingSet(a); err != nil { // once
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".config", "git-issue.tony"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), a) != 1 {
		t.Errorf("the config file holds %q", raw)
	}
	ws, err := startingSet(nil, &strings.Builder{})
	if err != nil {
		t.Fatal(err)
	}
	if got := ws.names(); got != "a, b" || ws.list()[0].From != "config" {
		t.Errorf("from config: %q, from %s", got, ws.list()[0].From)
	}

	// -C wins over the config.
	ws, err = startingSet([]string{b}, &strings.Builder{})
	if err != nil {
		t.Fatal(err)
	}
	if got := ws.names(); got != "b" {
		t.Errorf("from -C: %q", got)
	}

	// The working directory, when there is no config.
	if err := os.Remove(filepath.Join(home, ".config", "git-issue.tony")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(a)
	ws, err = startingSet(nil, &strings.Builder{})
	if err != nil {
		t.Fatal(err)
	}
	if got := ws.list(); len(got) != 1 || got[0].From != "cwd" || got[0].Name != "a" {
		t.Errorf("from cwd: %+v", got[0])
	}
}
