package ops

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/signadot/tony-format/git-issue/issuelib"
)

// repoAt makes a repository in a directory the process does not chdir into, and
// a store on it.
func repoAt(t *testing.T) (string, issuelib.Store) {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"commit", "-q", "--allow-empty", "-m", "root"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return dir, issuelib.NewGitStoreAt(dir, &strings.Builder{})
}

// TestExtReference: another repository's issue mirrored here resolves here --
// by id, by prefix, in a full listing, with its status as the source wrote it --
// is read-only here, and a relation to it is whole from this repository alone
// (a3v2a4j6h12kse4ypxn0).
func TestExtReference(t *testing.T) {
	bDir, b := repoAt(t)
	_, a := repoAt(t)

	far, err := Create(b, "Far away", "In b.")
	if err != nil {
		t.Fatal(err)
	}
	near, err := Create(a, "Near", "In a.")
	if err != nil {
		t.Fatal(err)
	}

	if err := SourceAdd(a, "b/c", bDir); err == nil {
		t.Error("a source name with a slash was accepted")
	}
	if _, err := Mirror(a, "b", far.ID); err == nil || !strings.Contains(err.Error(), "no source named") {
		t.Errorf("a mirror from an unknown source was accepted: %v", err)
	}
	if err := SourceAdd(a, "b", bDir); err != nil {
		t.Fatal(err)
	}
	if _, err := Mirror(a, "b", near.ID); err == nil || !strings.Contains(err.Error(), "own issue") {
		t.Errorf("mirroring one's own issue was accepted: %v", err)
	}
	if _, err := Mirror(a, "b", far.ID[:4]); err == nil {
		t.Error("a mirror by prefix was accepted")
	}

	mirrored, err := Mirror(a, "b", far.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mirrored.ID != far.ID || mirrored.Title != "Far away" || !issuelib.IsExtRef(mirrored.Ref) {
		t.Fatalf("mirrored as %+v", mirrored)
	}

	// It resolves here, by id and by prefix.
	ref, err := a.FindRef(far.ID)
	if err != nil || ref != issuelib.ExtRefForXIDR("b", far.ID) {
		t.Errorf("by id: %q, %v", ref, err)
	}
	if ref, err := a.FindRef(far.ID[:5]); err != nil || !issuelib.IsExtRef(ref) {
		t.Errorf("by prefix: %q, %v", ref, err)
	}
	sh, err := Show(a, far.ID)
	if err != nil || sh.Status != "open" || sh.Body != "In b." {
		t.Errorf("shown as %+v, %v", sh, err)
	}
	if open, _ := List(a, false, ""); len(open) != 1 || open[0].ID != near.ID {
		t.Errorf("the open list holds the mirror: %d issues", len(open))
	}
	if all, _ := List(a, true, ""); len(all) != 2 {
		t.Errorf("the full list has %d issues, want the mirror too", len(all))
	}

	// Read-only here.
	for name, write := range map[string]func() error{
		"comment": func() error { _, _, err := Comment(a, far.ID, "no"); return err },
		"edit":    func() error { _, err := Edit(a, far.ID, str("no"), nil); return err },
		"label":   func() error { _, err := Label(a, far.ID, []string{"no"}, nil); return err },
		"close":   func() error { _, err := Close(a, far.ID, ""); return err },
		"link":    func() error { _, _, err := Link(a, far.ID, "HEAD"); return err },
		"relate from": func() error {
			_, _, _, err := Relate(a, far.ID, near.ID, Related)
			return err
		},
	} {
		if err := write(); err == nil || !strings.Contains(err.Error(), "mirror of b's") {
			t.Errorf("%s of a mirror: %v", name, err)
		}
	}

	// A relation to it, whole from here.
	if _, _, changed, err := Relate(a, near.ID, far.ID, Blocks); err != nil || !changed {
		t.Fatalf("relate to the mirror: %v", err)
	}
	sh, _ = Show(a, near.ID)
	if len(sh.Blocks) != 1 || sh.Blocks[0].ID != far.ID || sh.Blocks[0].Title != "Far away" || sh.Blocks[0].Err != "" {
		t.Errorf("the relation reads as %+v", sh.Blocks)
	}
	// blocked_by is the far side's, and the far side is not written here.
	if sh, _ := Show(a, far.ID); len(sh.BlockedBy) != 0 {
		t.Errorf("the mirror was written: %+v", sh.BlockedBy)
	}

	// The source moves; a refresh follows it, and the status is what it wrote.
	if _, err := Close(b, far.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Comment(b, far.ID, "closing note"); err != nil {
		t.Fatal(err)
	}
	if n, err := Refresh(a, "b"); err != nil || n != 1 {
		t.Fatalf("refresh: %d, %v", n, err)
	}
	sh, _ = Show(a, far.ID)
	if sh.Status != "closed" || len(sh.Comments) != 1 {
		t.Errorf("after refresh: %s, %d comments", sh.Status, len(sh.Comments))
	}
	srcs, _ := Sources(a)
	if len(srcs) != 1 || srcs[0].Name != "b" || srcs[0].URL != bDir || srcs[0].Fetched.IsZero() {
		t.Errorf("sources: %+v", srcs)
	}

	// Gone, and the relation with it.
	if touched, err := Unmirror(a, far.ID); err != nil || touched != 1 {
		t.Fatalf("unmirror: %d, %v", touched, err)
	}
	if _, err := a.FindRef(far.ID); err == nil {
		t.Error("the mirror is still there")
	}
	if sh, _ := Show(a, near.ID); len(sh.Blocks) != 0 {
		t.Errorf("the relation outlived the mirror: %+v", sh.Blocks)
	}
	if _, err := Unmirror(a, near.ID); err == nil {
		t.Error("unmirroring one's own issue was accepted")
	}
}
