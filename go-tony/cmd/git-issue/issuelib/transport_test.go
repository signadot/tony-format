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

// TestSyncAnswersWhatFailed: a refspec git refused is an error the caller can act
// on. Both directions used to report it as a warning and answer nil, so a push
// that reached nothing and one that worked were the same to a script, and pull
// printed "Done." after either.
func TestSyncAnswersWhatFailed(t *testing.T) {
	gitInitWithOrigin(t)
	s := NewGitStoreWithOutput(&strings.Builder{})
	if _, err := s.Create("one", "# one\n"); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "remote", "add", "broken",
		filepath.Join(t.TempDir(), "not-a-repository")).CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v: %s", err, out)
	}

	spec := "+refs/issues/*:refs/issues/*"
	err := s.Push("broken", []string{spec})
	if err == nil {
		t.Fatal("a push at a remote that is not there was answered as a success")
	}
	if !strings.Contains(err.Error(), spec) {
		t.Errorf("the error does not name the refspec: %v", err)
	}
	if err := s.Fetch("broken", []string{spec}); err == nil {
		t.Error("a fetch from a remote that is not there was answered as a success")
	} else if !strings.Contains(err.Error(), spec) {
		t.Errorf("the error does not name the refspec: %v", err)
	}

	// Every refspec is attempted, and every failure is in the answer.
	err = s.Push("broken", []string{spec, "+refs/closed/*:refs/closed/*", "+refs/notes/issues:refs/notes/issues"})
	if err == nil {
		t.Fatal("pushing three refspecs at a remote that is not there succeeded")
	}
	// Two of the three: a glob is expanded against the remote's refs, so both
	// globs reach the connection and fail on it, while refs/notes/issues is
	// resolved locally first, matches nothing here, and is quiet.
	if got := strings.Count(err.Error(), "failed to push"); got != 2 {
		t.Errorf("the error names %d failures, want 2: %v", got, err)
	}
}

// TestSyncIsQuietAboutNothingToDo: what is not a failure stays quiet. A refspec
// matching nothing locally has nothing to send; a ref the remote does not have is
// already in the state a fetch or a deletion wanted.
func TestSyncIsQuietAboutNothingToDo(t *testing.T) {
	gitInitWithOrigin(t)
	s := NewGitStoreWithOutput(&strings.Builder{})
	if err := s.Push("origin", []string{
		"+refs/closed/*:refs/closed/*", // nothing local matches
		":refs/issues/gone",            // the remote does not have it
	}); err != nil {
		t.Errorf("a push with nothing to do failed: %v", err)
	}
	if err := s.Fetch("origin", []string{
		"+refs/issues/*:refs/issues/*",         // the remote has no issues
		"+refs/notes/issues:refs/notes/issues", // nor a reverse index
	}); err != nil {
		t.Errorf("a fetch with nothing to fetch failed: %v", err)
	}
}
