package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/signadot/tony-format/git-issue/issuelib"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// cloneOf makes a second working repository on the same origin, as another
// person's clone, and a store on it.
func cloneOf(t *testing.T, origin string) (string, issuelib.Store) {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.email", "other@example.com")
	gitIn(t, dir, "config", "user.name", "Other")
	gitIn(t, dir, "remote", "add", "origin", origin)
	return dir, issuelib.NewGitStoreAt(dir, &strings.Builder{})
}

func refsUnder(t *testing.T, dir, prefix string) []string {
	t.Helper()
	out := strings.TrimSpace(gitIn(t, dir, "for-each-ref", "--format=%(refname)", prefix))
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// TestSyncCarriesMirrors: a mirror is this repository's data. Push carries it,
// and its source, to this repository's origin and never to the source; a clone
// of this repository alone then follows the relation; a pull refreshes the
// mirror from its source, and the next push carries the refresh on. The source
// is carried too, so a directory given for it is recorded as the directory's
// origin, which the clone fetches from, while this clone keeps the directory
// (p76fwh8gh12kr1ppvdn0).
func TestSyncCarriesMirrors(t *testing.T) {
	bDir, b := repoAt(t)
	aDir, a := repoAt(t)
	origin := t.TempDir()
	gitIn(t, origin, "init", "-q", "--bare")
	gitIn(t, aDir, "remote", "add", "origin", origin)
	bOrigin := t.TempDir()
	gitIn(t, bOrigin, "init", "-q", "--bare")
	gitIn(t, bDir, "remote", "add", "origin", bOrigin)

	far, err := Create(b, "Far", "In b.")
	if err != nil {
		t.Fatal(err)
	}
	if report, err := Push(b, "origin", "", false, false); err != nil || report.Err() != nil {
		t.Fatalf("b's push: %v, %v", err, report.Err())
	}
	near, err := Create(a, "Near", "In a.")
	if err != nil {
		t.Fatal(err)
	}
	// A directory inside b is not b: git would find b from it, but its origin
	// is not where the directory's issues are, so it is recorded as it is.
	sub := filepath.Join(bDir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SourceAdd(a, "b", sub); err != nil {
		t.Fatal(err)
	}
	if src, err := a.Source("b"); err != nil || src.URL != sub || src.Path != "" {
		t.Fatalf("a directory inside a repository is recorded as %+v, %v; want it as it is", src, err)
	}
	if err := SourceAdd(a, "b", "file://"+bDir); err != nil {
		t.Fatal(err)
	}
	if srcs, err := a.Sources(); err != nil || len(srcs) != 1 || srcs[0].URL != bOrigin || srcs[0].Path != bDir {
		t.Fatalf("a directory given for a source is recorded as %+v, %v; want its origin %s with the directory kept here", srcs, err, bOrigin)
	}
	if _, err := Mirror(a, "b", far.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Relate(a, near.ID, far.ID, Blocks); err != nil {
		t.Fatal(err)
	}
	bRefsBefore := refsUnder(t, bDir, "refs/")

	// A dry push names the mirror and the source among what it would send.
	dry, err := Push(a, "origin", "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range dry.Changed {
		names = append(names, c.ID)
	}
	joined := strings.Join(names, "|")
	if !strings.Contains(joined, "ext b/"+far.ID) || !strings.Contains(joined, "source b") {
		t.Errorf("a dry push would send %v", names)
	}

	report, err := Push(a, "origin", "", false, false)
	if err != nil || report.Err() != nil {
		t.Fatalf("push: %v, %v", err, report.Err())
	}
	if got := refsUnder(t, origin, issuelib.ExtPrefix); len(got) != 1 || got[0] != issuelib.ExtRefForXIDR("b", far.ID) {
		t.Errorf("origin holds mirrors %v", got)
	}
	if got := refsUnder(t, origin, issuelib.SourcesPrefix); len(got) != 1 {
		t.Errorf("origin holds sources %v", got)
	}
	if after := refsUnder(t, bDir, "refs/"); strings.Join(after, ",") != strings.Join(bRefsBefore, ",") {
		t.Errorf("the source was written by a push: %v", after)
	}

	// A clone of a alone follows the relation.
	cDir, c := cloneOf(t, origin)
	if report, err := Pull(c, "origin", false, false); err != nil || report.Err() != nil {
		t.Fatalf("clone pull: %v, %v", err, report.Err())
	}
	sh, err := Show(c, near.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sh.Blocks) != 1 || sh.Blocks[0].Title != "Far" || sh.Blocks[0].Err != "" {
		t.Errorf("in the clone the relation reads as %+v", sh.Blocks)
	}
	if got := refsUnder(t, cDir, issuelib.SourcesPrefix); len(got) != 1 {
		t.Errorf("the clone holds sources %v", got)
	}

	// The source moves; a's pull refreshes the mirror; the push carries it on;
	// the clone's pull brings it forward -- from b's origin, which is what the
	// source record says, since b's directory is a's alone.
	if _, err := Close(b, far.ID, ""); err != nil {
		t.Fatal(err)
	}
	if report, err := Push(b, "origin", "", false, false); err != nil || report.Err() != nil {
		t.Fatalf("b's second push: %v, %v", err, report.Err())
	}
	report, err = Pull(a, "origin", false, false)
	if err != nil || report.Err() != nil || report.Refreshed["b"] != 1 {
		t.Fatalf("a's pull: %v, %v, refreshed %v", err, report.Err(), report.Refreshed)
	}
	if sh, _ := Show(a, far.ID); sh.Status != "closed" {
		t.Errorf("after refresh a reads the mirror as %s", sh.Status)
	}
	if report, err := Push(a, "origin", "", false, false); err != nil || report.Err() != nil {
		t.Fatalf("a's second push: %v, %v", err, report.Err())
	}
	report, err = Pull(c, "origin", false, false)
	if err != nil || report.Err() != nil {
		t.Fatalf("clone's second pull: %v, %v", err, report.Err())
	}
	// The clone's pull refreshed from the source too -- the same point.
	if sh, _ := Show(c, far.ID); sh.Status != "closed" {
		t.Errorf("the clone reads the mirror as %s", sh.Status)
	}

	// A source that cannot be reached is said, and does not fail the pull; and
	// what is said names where the url came from -- the shared record, or this
	// clone's own path -- so that a wrong one is found where it is written.
	if err := SourceAdd(a, "b", t.TempDir()+"/nowhere"); err != nil {
		t.Fatal(err)
	}
	report, err = Pull(a, "origin", false, false)
	if err != nil || report.Err() != nil {
		t.Fatalf("pull with an unreachable source: %v, %v", err, report.Err())
	}
	if why, ok := report.Unreached["b"]; !ok || !strings.Contains(why, issuelib.SourceRef("b")+" records") {
		t.Errorf("the unreachable source was not named by its record: %+v", report.Unreached)
	}
	gitIn(t, aDir, "config", "git-issue.source.b.path", t.TempDir()+"/moved")
	report, err = Pull(a, "origin", false, false)
	if err != nil || report.Err() != nil {
		t.Fatalf("pull with a stale path: %v, %v", err, report.Err())
	}
	if why, ok := report.Unreached["b"]; !ok || !strings.Contains(why, "git config git-issue.source.b.path") || !strings.Contains(why, "not a checkout of") {
		t.Errorf("the stale path was not named by its config key: %+v", report.Unreached)
	}
}
