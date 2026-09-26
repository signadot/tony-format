package issuelib

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// gitInit makes an empty repository in a temporary directory and makes it the
// process's working directory for the test: a GitStore holds no path, and works
// on the repository the git binary finds from there.
func gitInit(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

// TestConcurrentUpdatesAllLand: N writers adding a file each to one issue at
// once all get their commit onto the chain. The ref was written with no word of
// what it was expected to hold, so a writer that read the tip, built its commit
// and wrote the ref overwrote any writer that had done the same in between:
// eight comments at once all said "Added comment" and one survived
// (05d8w3cjh12kswb1msn0). The write now says what tip it built on, and a writer
// that finds the tip moved rebuilds on the new one.
func TestConcurrentUpdatesAllLand(t *testing.T) {
	gitInit(t)
	s := NewGitStoreWithOutput(&strings.Builder{})
	issue, err := s.Create("race", "# race\n")
	if err != nil {
		t.Fatal(err)
	}
	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = s.updateCommit(issue.Ref, fmt.Sprintf("comment %d", i),
				map[string]string{fmt.Sprintf("discussion/%d.md", i): fmt.Sprintf("comment %d\n", i)})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("writer %d: %v", i, err)
		}
	}
	files, err := s.ListDir(issue.Ref, "discussion")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if _, ok := files[fmt.Sprintf("%d.md", i)]; !ok {
			t.Errorf("writer %d's file is not in the tree: %v", i, files)
		}
	}
	out, err := exec.Command("git", "rev-list", "--count", issue.Ref).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != fmt.Sprint(n+1) {
		t.Errorf("the chain has %s commits, want %d (create + %d updates)", got, n+1, n)
	}
}
