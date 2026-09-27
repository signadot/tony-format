package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

// runRoot runs the command args name under Root, as the dispatcher would but
// for exiting the process, and answers what it printed.
func runRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cc, out := sayCC()
	cmd := Root()
	for len(args) > 0 && cmd.Hooks.Run == nil {
		rest, err := cmd.Parse(cc, args)
		if err != nil {
			return "", err
		}
		if len(rest) == 0 {
			break
		}
		sub := cmd.FindSub(cc, rest[0])
		if sub == nil {
			return "", fmt.Errorf("no command %q", rest[0])
		}
		cmd, args = sub, rest[1:]
	}
	err := cmd.Run(cc, args)
	return out.String(), err
}

// configured keeps the developer's own configuration out, and makes the
// configured set the directories named. It leaves the test in a directory
// that is no repository.
func configured(t *testing.T, dirs ...string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	if len(dirs) > 0 {
		cfg := "repos:\n"
		for _, dir := range dirs {
			cfg += "- " + dir + "\n"
		}
		if err := os.MkdirAll(filepath.Join(home, ".config"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".config", "git-issue.tony"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(t.TempDir())
}

// TestSet_OutsideARepositoryWithNoSet: with no repository and no set, a
// command that needs a repository says there is none, whatever it would have
// failed on first; help and version answer anywhere (eahqxymwh12ks32vq1n0).
func TestSet_OutsideARepositoryWithNoSet(t *testing.T) {
	configured(t)
	for _, args := range [][]string{
		{"create", "t", "--body", "b"},
		{"show", "abcd"},
		{"ext", "add", "s", "https://example.com/x"},
		{"ext", "list"},
		{"ext", "refresh"},
		{"push", "--all"},
		{"pull"},
		{"list"},
		{"for-commit", "HEAD"},
		{"serve"},
		{"migrate", "--dry-run"},
	} {
		if _, err := runRoot(t, args...); err == nil || !strings.Contains(err.Error(), "not a git repository") {
			t.Errorf("%v: %v, want not a git repository", args, err)
		}
	}
	if out, err := runRoot(t, "version"); err != nil || out == "" {
		t.Errorf("version outside a repository: %v, %q", err, out)
	}
	if _, err := runRoot(t, "create", "-h"); err != nil && strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("create -h outside a repository: %v", err)
	}
}

// TestSet_OutsideARepository: run outside a repository, a command given an
// issue runs on the repository of the set that holds it; one given none needs
// --repo, and says which there are; list covers every repository
// (bqgb1c8zh12ksh2xq1n0).
func TestSet_OutsideARepository(t *testing.T) {
	aDir, bDir := repoDir(t, "a"), repoDir(t, "b")
	a := issuelib.NewGitStoreAt(aDir, &strings.Builder{})
	b := issuelib.NewGitStoreAt(bDir, &strings.Builder{})
	inA, err := ops.Create(a, "In a", "body")
	if err != nil {
		t.Fatal(err)
	}
	inB, err := ops.Create(b, "In b", "body")
	if err != nil {
		t.Fatal(err)
	}
	configured(t, aDir, bDir)

	if out, err := runRoot(t, "show", inB.ID[:8]); err != nil || !strings.Contains(out, "In b") {
		t.Errorf("show an issue of b: %v, %q", err, out)
	}
	// A comment that begins with a dash is a comment, not an option.
	if _, err := runRoot(t, "comment", inB.ID, "- from anywhere"); err != nil {
		t.Fatalf("comment an issue of b: %v", err)
	}
	if sh, err := ops.Show(b, inB.ID); err != nil || len(sh.Comments) != 1 || !strings.Contains(sh.Comments[0].Text, "- from anywhere") {
		t.Errorf("b's issue after the comment: %v, %+v", err, sh)
	}
	if _, err := runRoot(t, "close", inA.ID); err != nil {
		t.Fatalf("close an issue of a: %v", err)
	}
	if sh, err := ops.Show(a, inA.ID); err != nil || sh.Status != "closed" {
		t.Errorf("a's issue after the close: %v, %+v", err, sh)
	}
	if _, err := runRoot(t, "show", "zzzzzzzz"); err == nil || !strings.Contains(err.Error(), "issue not found") {
		t.Errorf("an issue of no repository: %v", err)
	}

	_, err = runRoot(t, "create", "Nowhere", "--body", "b")
	if err == nil || !strings.Contains(err.Error(), "--repo") || !strings.Contains(err.Error(), "a, b") {
		t.Errorf("create with no repository named: %v", err)
	}
	for _, args := range [][]string{
		{"create", "--repo", "b", "Made in b", "--body", "b"},
		{"--repo", "b", "create", "Made in b too", "--body", "b"},
		{"create", "Made in b by its directory", "--body", "b", "--repo=" + bDir},
	} {
		if out, err := runRoot(t, args...); err != nil || !strings.Contains(out, "Created issue") {
			t.Errorf("%v: %v, %q", args, err, out)
		}
	}
	if issues, err := ops.List(b, false, ""); err != nil || len(issues) != 4 {
		t.Errorf("b holds %d issue(s), %v; want the 4 made in it", len(issues), err)
	}
	if _, err := runRoot(t, "create", "--repo", "c", "t", "--body", "b"); err == nil || !strings.Contains(err.Error(), `"c"`) {
		t.Errorf("a repository of no set: %v", err)
	}
	if _, err := runRoot(t, "ext", "add", "--repo", "a", "far", "https://example.com/far"); err != nil {
		t.Errorf("ext add in a: %v", err)
	}
	if srcs, err := a.Sources(); err != nil || len(srcs) != 1 || srcs[0].Name != "far" {
		t.Errorf("a's sources: %v, %+v", err, srcs)
	}

	out, err := runRoot(t, "list", "--all")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"a  " + inA.ID, "b  " + inB.ID} {
		if !strings.Contains(out, want) {
			t.Errorf("list does not say %q:\n%s", want, out)
		}
	}
	if out, err := runRoot(t, "list", "--repo", "a", "--all"); err != nil || strings.Contains(out, inB.ID) || !strings.HasPrefix(out, inA.ID) {
		t.Errorf("list of a: %v\n%s", err, out)
	}

	if _, err := runRoot(t, "migrate", "--repo", "a", "--dry-run"); err == nil || !strings.Contains(err.Error(), "--repo") {
		t.Errorf("migrate with --repo: %v", err)
	}
}

// TestSet_InsideARepository: run inside a repository, a command runs on it as
// it did, whatever the set; an issue it does not hold is found in the set;
// and a relation to an issue of another repository mirrors it here first.
func TestSet_InsideARepository(t *testing.T) {
	aDir, bDir := repoDir(t, "a"), repoDir(t, "b")
	a := issuelib.NewGitStoreAt(aDir, &strings.Builder{})
	b := issuelib.NewGitStoreAt(bDir, &strings.Builder{})
	inA, err := ops.Create(a, "In a", "body")
	if err != nil {
		t.Fatal(err)
	}
	inB, err := ops.Create(b, "In b", "body")
	if err != nil {
		t.Fatal(err)
	}
	configured(t, aDir, bDir)
	sub := filepath.Join(aDir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	if out, err := runRoot(t, "create", "Made here", "--body", "b"); err != nil || !strings.Contains(out, "Created issue") {
		t.Fatalf("create in a: %v, %q", err, out)
	}
	if issues, err := ops.List(a, false, ""); err != nil || len(issues) != 2 {
		t.Errorf("a holds %d issue(s), %v; want 2", len(issues), err)
	}
	if out, err := runRoot(t, "list"); err != nil || strings.Contains(out, inB.ID) || strings.Contains(out, "a  ") {
		t.Errorf("list in a: %v\n%s", err, out)
	}
	if out, err := runRoot(t, "show", inB.ID); err != nil || !strings.Contains(out, "In b") {
		t.Errorf("show an issue of b from a: %v, %q", err, out)
	}

	// A source a already has under b's name is left as it was recorded, and
	// is where the mirror comes from.
	recorded := "file://" + bDir
	if err := ops.SourceAdd(a, "b", recorded); err != nil {
		t.Fatal(err)
	}
	out, err := runRoot(t, "blocks", inA.ID, inB.ID)
	if err != nil {
		t.Fatalf("blocks across repositories: %v", err)
	}
	if !strings.Contains(out, "Mirrored "+inB.ID+" from b") {
		t.Errorf("blocks said %q", out)
	}
	if ref, err := a.FindRef(inB.ID); err != nil || ref != issuelib.ExtRefForXIDR("b", inB.ID) {
		t.Errorf("a holds b's issue at %q, %v", ref, err)
	}
	if srcs, err := a.Sources(); err != nil || len(srcs) != 1 || srcs[0].URL != recorded {
		t.Errorf("a's source after blocks: %v, %+v; want it at %s", err, srcs, recorded)
	}
	if sh, err := ops.Show(a, inA.ID); err != nil || len(sh.Blocks) != 1 || sh.Blocks[0].Title != "In b" {
		t.Errorf("a's issue after blocks: %v, %+v", err, sh)
	}
	// The mirror is here now, so the issue is shown from here, as a mirror.
	if out, err := runRoot(t, "show", inB.ID); err != nil || !strings.Contains(out, "Mirror of b") {
		t.Errorf("show the mirrored issue from a: %v, %q", err, out)
	}
}

func TestCutRepo(t *testing.T) {
	for _, c := range []struct {
		args, rest []string
		name       string
	}{
		{[]string{"id", "text"}, []string{"id", "text"}, ""},
		{[]string{"--repo", "a", "id"}, []string{"id"}, "a"},
		{[]string{"id", "-repo=a", "- text"}, []string{"id", "- text"}, "a"},
		{[]string{"id", "--", "--repo", "a"}, []string{"id", "--", "--repo", "a"}, ""},
		{[]string{"--force", "id"}, []string{"--force", "id"}, ""},
	} {
		rest, name, err := cutRepo(c.args)
		if err != nil || name != c.name || !reflect.DeepEqual(rest, c.rest) {
			t.Errorf("cutRepo(%q) = %q, %q, %v; want %q, %q", c.args, rest, name, err, c.rest, c.name)
		}
	}
	if _, _, err := cutRepo([]string{"id", "--repo"}); err == nil {
		t.Error("--repo with no name was taken")
	}
}

var _ = cli.ErrUsage
