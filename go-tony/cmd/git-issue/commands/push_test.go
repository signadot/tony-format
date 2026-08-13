package commands

import (
	"os/exec"
	"sort"
	"strings"
	"testing"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

// nopWriteCloser lets a strings.Builder stand in for cli.Context's output.
type nopWriteCloser struct{ *strings.Builder }

func (nopWriteCloser) Close() error { return nil }

// pushTestRepo returns a store on a fresh repository with a bare "origin" to
// push at, and the path of that origin.
func pushTestRepo(t *testing.T) (issuelib.Store, string) {
	t.Helper()
	origin := t.TempDir()
	run(t, "", "init", "-q", "--bare", origin)
	store := testRepo(t) // chdirs into the working repository
	run(t, "", "remote", "add", "origin", origin)
	return store, origin
}

// run runs git in dir, or in the process's working directory when dir is empty.
func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// remoteIssueRefs returns the issue refs the bare repository at origin holds.
func remoteIssueRefs(t *testing.T, origin string) []string {
	t.Helper()
	out := run(t, origin, "for-each-ref", "--format=%(refname)", "refs/issues/*", "refs/closed/*")
	var refs []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			refs = append(refs, line)
		}
	}
	sort.Strings(refs)
	return refs
}

// moveIssue closes or reopens an issue exactly as the close and reopen commands
// do: commit the metadata change, then move the ref.
func moveIssue(t *testing.T, store issuelib.Store, id, status string) {
	t.Helper()
	ref, err := store.FindRef(id)
	if err != nil {
		t.Fatalf("failed to find ref for %s: %v", id, err)
	}
	issue, _, err := store.GetByRef(ref)
	if err != nil {
		t.Fatalf("failed to read %s: %v", id, err)
	}
	issue.Status = status
	if err := store.Update(issue, status, nil); err != nil {
		t.Fatalf("failed to update %s: %v", id, err)
	}
	to := issuelib.RefForXIDR(id)
	if status == "closed" {
		to = issuelib.ClosedRefForXIDR(id)
	}
	if err := store.MoveRef(ref, to); err != nil {
		t.Fatalf("failed to move %s: %v", id, err)
	}
}

func pushCC() *cli.Context {
	return &cli.Context{Out: nopWriteCloser{&strings.Builder{}}, Err: nopWriteCloser{&strings.Builder{}}}
}

// TestPush_MirrorsStatusMove: a status change moves the ref, so a push of it has
// to move the remote's, not add a second one. A remote holding an id in both
// namespaces cannot be read for a status at all: a close and a reopen leave the
// same pair behind.
func TestPush_MirrorsStatusMove(t *testing.T) {
	store, origin := pushTestRepo(t)
	cfg := &pushConfig{store: store}
	cc := pushCC()

	issue, err := store.Create("Movable", "# Movable\n\nbody\n")
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	id := issue.ID

	for _, tc := range []struct {
		name, status string
		want         []string
	}{
		{"open", "", []string{"refs/issues/" + id}},
		{"closed", "closed", []string{"refs/closed/" + id}},
		{"reopened", "open", []string{"refs/issues/" + id}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.status != "" {
				moveIssue(t, store, id, tc.status)
			}
			if err := cfg.pushSingle(cc, "origin", id); err != nil {
				t.Fatalf("push: %v", err)
			}
			if got := remoteIssueRefs(t, origin); !equal(got, tc.want) {
				t.Fatalf("after push of %s issue, origin holds %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestPushAll_MirrorsStatusMove: --all is what an operator reaches for after a
// batch of closes, and the wildcard refspecs it pushes only ever add refs.
func TestPushAll_MirrorsStatusMove(t *testing.T) {
	store, origin := pushTestRepo(t)
	cfg := &pushConfig{store: store}
	cc := pushCC()

	kept, err := store.Create("Kept open", "# Kept open\n\nbody\n")
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	closed, err := store.Create("To close", "# To close\n\nbody\n")
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	if err := cfg.pushAll(cc, "origin"); err != nil {
		t.Fatalf("push --all: %v", err)
	}
	moveIssue(t, store, closed.ID, "closed")
	if err := cfg.pushAll(cc, "origin"); err != nil {
		t.Fatalf("push --all: %v", err)
	}

	want := []string{"refs/closed/" + closed.ID, "refs/issues/" + kept.ID}
	sort.Strings(want)
	if got := remoteIssueRefs(t, origin); !equal(got, want) {
		t.Fatalf("origin holds %v, want %v", got, want)
	}
}

// TestPushAll_LeavesIssuesItDoesNotHave: pushing mirrors the moves this
// repository made, and nothing else. An issue only the remote has -- filed in
// another clone, or fetched and never here -- is not this push's business.
func TestPushAll_LeavesIssuesItDoesNotHave(t *testing.T) {
	store, origin := pushTestRepo(t)
	cfg := &pushConfig{store: store}
	cc := pushCC()

	theirs, err := store.Create("Theirs", "# Theirs\n\nbody\n")
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	mine, err := store.Create("Mine", "# Mine\n\nbody\n")
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	if err := cfg.pushAll(cc, "origin"); err != nil {
		t.Fatalf("push --all: %v", err)
	}

	// Forget theirs locally, then close and push mine.
	run(t, "", "update-ref", "-d", issuelib.RefForXIDR(theirs.ID))
	moveIssue(t, store, mine.ID, "closed")
	if err := cfg.pushAll(cc, "origin"); err != nil {
		t.Fatalf("push --all: %v", err)
	}

	want := []string{"refs/closed/" + mine.ID, "refs/issues/" + theirs.ID}
	sort.Strings(want)
	if got := remoteIssueRefs(t, origin); !equal(got, want) {
		t.Fatalf("origin holds %v, want %v", got, want)
	}
}

// TestPull_AdoptsStatusMove is the mirror image: fetching is additive too, so a
// close made in another clone arrives beside the open ref this one already has.
// Pull has to end at one ref, and at the one the close moved to.
func TestPull_AdoptsStatusMove(t *testing.T) {
	store, _ := pushTestRepo(t)
	push := &pushConfig{store: store}
	pull := &pullConfig{store: store}
	cc := pushCC()

	issue, err := store.Create("Closed elsewhere", "# Closed elsewhere\n\nbody\n")
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	openRef := issuelib.RefForXIDR(issue.ID)
	openCommit, err := store.GetRefCommit(openRef)
	if err != nil {
		t.Fatalf("failed to read ref commit: %v", err)
	}

	if err := push.pushAll(cc, "origin"); err != nil {
		t.Fatalf("push --all: %v", err)
	}
	moveIssue(t, store, issue.ID, "closed")
	if err := push.pushAll(cc, "origin"); err != nil {
		t.Fatalf("push --all: %v", err)
	}

	// Put back the open ref a clone that fetched before the close would hold.
	run(t, "", "update-ref", openRef, openCommit)
	if err := pull.run(cc, []string{"origin"}); err != nil {
		t.Fatalf("pull: %v", err)
	}

	got, err := store.ListRefs(true)
	if err != nil {
		t.Fatalf("failed to list refs: %v", err)
	}
	sort.Strings(got)
	if want := []string{issuelib.ClosedRefForXIDR(issue.ID)}; !equal(got, want) {
		t.Fatalf("after pull, repository holds %v, want %v", got, want)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
