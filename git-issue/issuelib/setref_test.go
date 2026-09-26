package issuelib

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// TestSetRefSaysWhatItExpected: a ref write names the tip it built on, and a
// create names no tip at all; either is refused when the ref disagrees.
func TestSetRefSaysWhatItExpected(t *testing.T) {
	gitInit(t)
	s := NewGitStoreWithOutput(&strings.Builder{})
	issue, err := s.Create("one", "# one\n")
	if err != nil {
		t.Fatal(err)
	}
	tip, err := s.GetRefCommit(issue.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.setRef(issue.Ref, tip, zeroSHA); err == nil {
		t.Error("a create over an existing ref was accepted")
	}
	if err := s.setRef(issue.Ref, tip, strings.Repeat("1", 40)); err == nil {
		t.Error("a write expecting the wrong tip was accepted")
	}
	if err := s.setRef(issue.Ref, tip, tip); err != nil {
		t.Errorf("a write expecting the tip it read was refused: %v", err)
	}
}

// TestIssueRefsAreLogged: what a forced sync overwrites is recoverable by
// ordinary means. Git logs refs/heads/, refs/remotes/, refs/notes/ and HEAD and
// nothing else by default, so an issue ref had no log at all and an overwritten
// tip was a dangling commit until gc took it. Every write the store makes asks
// for the log, and git keeps logging a ref whose log exists -- which is what
// covers an overwrite by someone else's fetch.
func TestIssueRefsAreLogged(t *testing.T) {
	gitInit(t)
	s := NewGitStoreWithOutput(&strings.Builder{})
	issue, err := s.Create("logged", "# logged\n")
	if err != nil {
		t.Fatal(err)
	}
	var tips []string
	at := func() string {
		t.Helper()
		tip, err := s.GetRefCommit(issue.Ref)
		if err != nil {
			t.Fatal(err)
		}
		return tip
	}
	tips = append(tips, at())
	for i := range 2 {
		if err := s.Update(issue, fmt.Sprintf("edit %d", i), nil); err != nil {
			t.Fatal(err)
		}
		tips = append(tips, at())
	}

	// An overwrite by another hand, which is what a forced fetch is.
	lost := tips[len(tips)-1]
	other, err := s.Create("other", "# other\n")
	if err != nil {
		t.Fatal(err)
	}
	otherTip, err := s.GetRefCommit(other.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "update-ref", issue.Ref, otherTip).CombinedOutput(); err != nil {
		t.Fatalf("overwrite: %v: %s", err, out)
	}

	logged := reflogTips(t, issue.Ref)
	for _, want := range append(tips, otherTip) {
		if !slices.Contains(logged, want) {
			t.Errorf("reflog of %s does not hold %s: %v", issue.Ref, want[:8], logged)
		}
	}
	if !slices.Contains(logged, lost) {
		t.Errorf("the overwritten tip %s is not recoverable", lost[:8])
	}
}

// reflogTips is every commit the ref's reflog records, newest first.
func reflogTips(t *testing.T, ref string) []string {
	t.Helper()
	out, err := exec.Command("git", "reflog", "show", "--format=%H", ref).Output()
	if err != nil {
		t.Fatalf("reflog show %s: %v", ref, err)
	}
	var tips []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			tips = append(tips, line)
		}
	}
	return tips
}
