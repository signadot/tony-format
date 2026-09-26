package issuelib

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitInitWithOrigin makes an empty repository with a bare "origin" to sync with,
// makes it the process's working directory, and answers origin's path.
func gitInitWithOrigin(t *testing.T) string {
	t.Helper()
	origin := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "--bare", origin).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, out)
	}
	gitInit(t)
	if out, err := exec.Command("git", "remote", "add", "origin", origin).CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v: %s", err, out)
	}
	return origin
}

// TestSyncAnswersWhatFailed: what git refused is an error the caller can act on.
// Both directions used to report it as a warning and answer nil, so a sync that
// reached nothing and one that worked were the same to a script, and pull printed
// "Done." after either.
func TestSyncAnswersWhatFailed(t *testing.T) {
	gitInitWithOrigin(t)
	s := NewGitStoreWithOutput(&strings.Builder{})
	issue, commits := chain(t, s, 1)
	if out, err := exec.Command("git", "remote", "add", "broken",
		filepath.Join(t.TempDir(), "not-a-repository")).CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v: %s", err, out)
	}

	if _, err := s.FetchTracking("broken"); err == nil {
		t.Error("a fetch from a remote that is not there was answered as a success")
	} else if !strings.Contains(err.Error(), "broken") {
		t.Errorf("the error does not name the remote: %v", err)
	}

	p := IssuePlan{
		XIDR:    issue.ID,
		Local:   &Tip{Ref: issue.Ref, Commit: commits[0]},
		Verdict: LocalOnly,
	}
	if _, err := s.ApplyPush("broken", p, false); err == nil {
		t.Error("a push at a remote that is not there was answered as a success")
	} else if !strings.Contains(err.Error(), FormatID(issue.ID)) {
		t.Errorf("the error does not name the issue: %v", err)
	}
}

// TestSyncIsQuietAboutNothingToDo: what is not a failure stays quiet. A remote
// with no issues, no closed issues and no reverse index is where every
// repository starts, and a glob matching nothing there is not an error.
func TestSyncIsQuietAboutNothingToDo(t *testing.T) {
	gitInitWithOrigin(t)
	s := NewGitStoreWithOutput(&strings.Builder{})

	migrated, err := s.FetchTracking("origin")
	if err != nil {
		t.Errorf("a fetch with nothing to fetch failed: %v", err)
	}
	if migrated {
		t.Error("a remote holding nothing was read as one of this generation")
	}

	// Pushing an issue makes it one, which is what the tripwire is read from.
	issue, commits := chain(t, s, 1)
	p := IssuePlan{
		XIDR:    issue.ID,
		Local:   &Tip{Ref: issue.Ref, Commit: commits[0]},
		Verdict: LocalOnly,
	}
	if _, err := s.ApplyPush("origin", p, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	if migrated, err := s.FetchTracking("origin"); err != nil || !migrated {
		t.Errorf("after a push the remote reads as migrated = %v, %v", migrated, err)
	}
}
