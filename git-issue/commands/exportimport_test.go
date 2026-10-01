package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

// AN ISSUE EXPORTS, IS EDITED AS FILES, AND IMPORTS BACK, run as the CLI runs it. Export and
// import asked for a GitStore and were handed the targetStore the commands share, so both
// refused every repository (8f4azhtjh12ksh2jqsn0).
func TestExportEditImport(t *testing.T) {
	dir := repoDir(t, "a")
	store := issuelib.NewGitStoreAt(dir, &strings.Builder{})
	made, err := ops.Create(store, "Rewrite me", "the first body")
	if err != nil {
		t.Fatal(err)
	}
	configured(t, dir)
	t.Chdir(dir)
	out := filepath.Join(t.TempDir(), "exported")
	if got, err := runRoot(t, "export", made.ID, out); err != nil || !strings.Contains(got, "Exported issue") {
		t.Fatalf("export: %v, %q", err, got)
	}
	desc := filepath.Join(out, "description.md")
	body, err := os.ReadFile(desc)
	if err != nil || !strings.Contains(string(body), "the first body") {
		t.Fatalf("the export's description: %v, %q", err, body)
	}
	rewritten := strings.Replace(string(body), "the first body", "the body rewritten", 1)
	if err := os.WriteFile(desc, []byte(rewritten), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := runRoot(t, "import", out); err != nil || !strings.Contains(got, "Imported issue") {
		t.Fatalf("import: %v, %q", err, got)
	}
	_, back, err := store.Get(made.ID)
	if err != nil || !strings.Contains(back, "the body rewritten") {
		t.Fatalf("after import the issue says %q (%v)", back, err)
	}
}

// THE EDITOR'S CONTEXT IS THE ISSUE: `comment` with no text exports the issue beside the editor,
// from the same store the commands share, and warned instead.
func TestTheCommentContextExportsThroughTheSharedStore(t *testing.T) {
	dir := repoDir(t, "a")
	store := issuelib.NewGitStoreAt(dir, &strings.Builder{})
	made, err := ops.Create(store, "Comment on me", "context")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.FindRef(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := ExportToTempDir(&targetStore{Store: store}, ref)
	if err != nil {
		t.Fatalf("exporting through the shared store: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(ctx) })
	if _, err := os.Stat(filepath.Join(ctx, "description.md")); err != nil {
		t.Fatalf("the context has no description: %v", err)
	}
}
