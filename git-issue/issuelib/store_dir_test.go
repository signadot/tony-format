package issuelib

import (
	"os/exec"
	"strings"
	"testing"
)

// refsAt lists refs in the process's working directory, for the tests that chdir
// into their repository and read its refs directly.
func refsAt(patterns ...string) []refAt {
	return (&GitStore{}).refsAt(patterns...)
}

// initRepoAt makes a repository in a directory the process does not chdir into.
func initRepoAt(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

// TestTwoStoresOnTwoDirectories: a store acts on the repository it was made for,
// and only that one. The store used to act on the process's working directory,
// so one process could hold one repository, which is what a server over several
// cannot live with (7qfhwth7h12ksxcwpxn0).
func TestTwoStoresOnTwoDirectories(t *testing.T) {
	a := NewGitStoreAt(initRepoAt(t), &strings.Builder{})
	b := NewGitStoreAt(initRepoAt(t), &strings.Builder{})

	inA, err := a.Create("in a", "# in a\n\nbody\n")
	if err != nil {
		t.Fatal(err)
	}
	inB, err := b.Create("in b", "# in b\n\nbody\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		s    *GitStore
		has  string
		not  string
	}{{"a", a, inA.ID, inB.ID}, {"b", b, inB.ID, inA.ID}} {
		if _, err := c.s.FindRef(c.has); err != nil {
			t.Errorf("%s cannot find its own issue: %v", c.name, err)
		}
		if _, err := c.s.FindRef(c.not); err == nil {
			t.Errorf("%s finds the other repository's issue", c.name)
		}
		issues, err := c.s.List(true)
		if err != nil || len(issues) != 1 || issues[0].ID != c.has {
			t.Errorf("%s lists %d issues (%v)", c.name, len(issues), err)
		}
	}
	// A write in one is a commit on one chain, and the other's is untouched.
	if _, _, err := a.Get(inA.ID); err != nil {
		t.Fatal(err)
	}
	issue, _, _ := a.Get(inA.ID)
	if err := a.Update(issue, "touch", map[string]string{"discussion/x.md": "x\n"}); err != nil {
		t.Fatal(err)
	}
	if files, _ := b.ListDir(inB.Ref, "discussion"); len(files) != 0 {
		t.Errorf("b grew a discussion from a's write: %v", files)
	}
}
