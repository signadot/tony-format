package commands

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

func hasPrefixed(lines []string, prefix string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

// TestMCP_IssueWatch: issue_watch answers a change made beside the server when
// the watch finds it, saying what was done; a change made between two calls is
// answered at once from the cursor; a change to an issue not watched is not
// answered; and at the timeout it answers none, with a cursor
// (vegmw7bmh12ks11mq1n0).
func TestMCP_IssueWatch(t *testing.T) {
	dir := repoDir(t, "one")
	beside := issuelib.NewGitStoreAt(dir, &strings.Builder{})
	watched, err := ops.Create(beside, "Watched", "b")
	if err != nil {
		t.Fatal(err)
	}
	other, err := ops.Create(beside, "Other", "b")
	if err != nil {
		t.Fatal(err)
	}
	cs, _ := mcpClientWatching(t, isolatedSet(t, dir), 20*time.Millisecond)

	// Waiting, then a comment from a shell.
	answered := make(chan watchOut, 1)
	go func() {
		var out watchOut
		call(t, cs, "issue_watch", map[string]any{"ids": []string{watched.ID[:8]}, "timeout": 10}, &out)
		answered <- out
	}()
	time.Sleep(100 * time.Millisecond)
	if _, _, err := ops.Comment(beside, other.ID, "not watched"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, _, err := ops.Comment(beside, watched.ID, "from a shell"); err != nil {
		t.Fatal(err)
	}
	var out watchOut
	select {
	case out = <-answered:
	case <-time.After(5 * time.Second):
		t.Fatal("issue_watch did not answer the comment")
	}
	if len(out.Changes) != 1 || out.Changes[0].ID != watched.ID {
		t.Fatalf("answered %+v, want only %s", out.Changes, watched.ID)
	}
	if !hasPrefixed(out.Changes[0].What, "comment") {
		t.Errorf("what = %q, want the comment's commit", out.Changes[0].What)
	}

	// Closed through the server between two calls: the cursor answers it at once.
	call(t, cs, "issue_close", map[string]any{"id": watched.ID}, nil)
	var next watchOut
	start := time.Now()
	call(t, cs, "issue_watch", map[string]any{"ids": []string{watched.ID}, "since": out.Cursor, "timeout": 10}, &next)
	if time.Since(start) > 2*time.Second {
		t.Errorf("a change before the call waited %v", time.Since(start))
	}
	if len(next.Changes) != 1 || next.Changes[0].Status != "closed" || strings.Join(next.Changes[0].What, "; ") != "close" {
		t.Fatalf("answered %+v, want the close, said once", next.Changes)
	}

	// Nothing more: the timeout answers none, and a cursor.
	var none watchOut
	res := call(t, cs, "issue_watch", map[string]any{"since": next.Cursor, "timeout": 1}, &none)
	if len(none.Changes) != 0 || none.Cursor != next.Cursor {
		t.Errorf("at the timeout: %+v, want no change at cursor %s", none, next.Cursor)
	}
	if !strings.Contains(text(res), "No change") {
		t.Errorf("text %q", text(res))
	}

	// A cursor from another server is refused, not read as this one's.
	parts := strings.Split(next.Cursor, ".")
	for _, c := range []string{"0" + next.Cursor, parts[0] + ".999." + parts[2], "garbage"} {
		if msg := refused(t, cs, "issue_watch", map[string]any{"since": c, "timeout": 1}); !strings.Contains(msg, "cursor") {
			t.Errorf("since %s: refusal %q", c, msg)
		}
	}
}

// TestWatchStore: `git issue watch`'s loop hands on each change to the issues
// it watches, with what was done -- a label, a close -- and only those
// carrying the label when one is given.
func TestWatchStore(t *testing.T) {
	store := testRepo(t)
	issue, err := ops.Create(store, "Watched", "b")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan watchChange, 16)
	done := make(chan error, 1)
	go func() {
		done <- watchStore(ctx, store, watchFilter{label: "bug"}, 10*time.Millisecond, nil, 0, func(ch watchChange) { got <- ch }, nil)
	}()
	time.Sleep(50 * time.Millisecond)
	if _, _, err := ops.Comment(store, issue.ID, "before the label"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := ops.Label(store, issue.ID, []string{"bug"}, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := ops.Close(store, issue.ID, ""); err != nil {
		t.Fatal(err)
	}
	// A close is a commit and then a move: one look may find it whole, or
	// two find its halves. Either way the last says closed.
	var changes []watchChange
	deadline := time.After(5 * time.Second)
	for len(changes) == 0 || changes[len(changes)-1].Status != "closed" {
		select {
		case ch := <-got:
			changes = append(changes, ch)
		case <-deadline:
			t.Fatalf("heard %+v, want the label and the close", changes)
		}
	}
	if hasPrefixed(changes[0].What, "comment") || !hasPrefixed(changes[0].What, "label") {
		t.Errorf("first = %q, want the label and not the unlabeled comment", changes[0].What)
	}
	// Found whole, the close is its commit alone; found in halves, the move
	// carries no commit, so it is said as closed.
	if last := changes[len(changes)-1]; len(last.What) != 1 || (last.What[0] != "close" && last.What[0] != "closed") {
		t.Errorf("last = %q, want the close said once", last.What)
	}

	// The label's removal is heard by a watcher on the label.
	if _, err := ops.Label(store, issue.ID, nil, []string{"bug"}); err != nil {
		t.Fatal(err)
	}
	select {
	case ch := <-got:
		if !hasPrefixed(ch.What, "label: removed") {
			t.Errorf("unlabel heard as %q", ch.What)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the label's removal was not heard")
	}
	cancel()
	if err := <-done; err != nil {
		t.Error(err)
	}
}

// TestWatchStore_Arrived: an issue new to the watcher lists only what was done
// to it since the watch began. One fetched in with an older history says it
// arrived, rather than bringing that history; one filed since lists its create.
func TestWatchStore_Arrived(t *testing.T) {
	dir, srcDir := repoDir(t, "here"), repoDir(t, "elsewhere")
	store := issuelib.NewGitStoreAt(dir, &strings.Builder{})
	elsewhere := issuelib.NewGitStoreAt(srcDir, &strings.Builder{})
	old, err := ops.Create(elsewhere, "Old elsewhere", "b")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ops.Comment(elsewhere, old.ID, "long ago"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond) // commit times are in seconds
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan watchChange, 16)
	go watchStore(ctx, store, watchFilter{}, 10*time.Millisecond, nil, 0, func(ch watchChange) { got <- ch }, nil)
	time.Sleep(50 * time.Millisecond)

	if out, err := exec.Command("git", "-C", dir, "fetch", "-q", srcDir,
		"refs/git-issues/*:refs/git-issues/*").CombinedOutput(); err != nil {
		t.Fatalf("fetch: %v: %s", err, out)
	}
	select {
	case ch := <-got:
		if ch.ID != old.ID || len(ch.What) != 1 || ch.What[0] != "arrived" {
			t.Errorf("fetched in: %+v, want only arrived", ch)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the fetched issue was not heard")
	}

	filed, err := ops.Create(store, "Filed since", "b")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ch := <-got:
		if ch.ID != filed.ID || !hasPrefixed(ch.What, "create") {
			t.Errorf("filed: %+v, want its create", ch)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the filed issue was not heard")
	}
}

// TestDescribe_MoveWithoutCommit: a ref brought across between namespaces with
// no commit of its own -- as a pull does -- is said as closed or reopened.
func TestDescribe_MoveWithoutCommit(t *testing.T) {
	dir := repoDir(t, "one")
	store := issuelib.NewGitStoreAt(dir, &strings.Builder{})
	issue, err := ops.Create(store, "Moved", "b")
	if err != nil {
		t.Fatal(err)
	}
	was, err := lookAt(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MoveRef(issuelib.OpenPrefix+issue.ID, issuelib.ClosedPrefix+issue.ID); err != nil {
		t.Fatal(err)
	}
	now, err := lookAt(store)
	if err != nil {
		t.Fatal(err)
	}
	ch, _ := describe(store, "", issue.ID, was[issue.ID], now[issue.ID], time.Now())
	if ch.Status != "closed" || strings.Join(ch.What, "; ") != "closed" {
		t.Errorf("moved without a commit: %+v, want closed", ch)
	}
	if err := store.MoveRef(issuelib.ClosedPrefix+issue.ID, issuelib.OpenPrefix+issue.ID); err != nil {
		t.Fatal(err)
	}
	back, err := lookAt(store)
	if err != nil {
		t.Fatal(err)
	}
	if ch, _ := describe(store, "", issue.ID, now[issue.ID], back[issue.ID], time.Now()); strings.Join(ch.What, "; ") != "reopened" {
		t.Errorf("moved back without a commit: %+v, want reopened", ch)
	}
}

// TestDescribe_MidMove: a look can land inside a move, when the ref is at
// both open and closed. The move is said once, as the status it moves to,
// and the look that finds the old ref gone has nothing more to say.
func TestDescribe_MidMove(t *testing.T) {
	dir := repoDir(t, "one")
	store := issuelib.NewGitStoreAt(dir, &strings.Builder{})
	issue, err := ops.Create(store, "Moving", "b")
	if err != nil {
		t.Fatal(err)
	}
	open, closed := issuelib.OpenPrefix+issue.ID, issuelib.ClosedPrefix+issue.ID
	before, err := lookAt(store)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "update-ref", closed, open).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	mid, err := lookAt(store)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "update-ref", "-d", open).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	after, err := lookAt(store)
	if err != nil {
		t.Fatal(err)
	}
	var said []string
	for i := 0; i < 20; i++ { // map order: the ref read must not be chosen by chance
		said = said[:0]
		for _, pair := range [][2]issueRefs{{before, mid}, {mid, after}} {
			if ch, ok := describe(store, "", issue.ID, pair[0][issue.ID], pair[1][issue.ID], time.Now()); ok {
				said = append(said, ch.Status+": "+strings.Join(ch.What, "; "))
			}
		}
		if strings.Join(said, " | ") != "closed: closed" {
			t.Fatalf("a move found in halves said %q, want it once as closed", said)
		}
	}
}

// TestMCP_IssueWatchRepo: a watch scoped to a repository is not answered by a
// change in another served one, and is by a change in its own; a repository
// not served is refused.
func TestMCP_IssueWatchRepo(t *testing.T) {
	oneDir, twoDir := repoDir(t, "one"), repoDir(t, "two")
	one := issuelib.NewGitStoreAt(oneDir, &strings.Builder{})
	two := issuelib.NewGitStoreAt(twoDir, &strings.Builder{})
	inOne, err := ops.Create(one, "In one", "b")
	if err != nil {
		t.Fatal(err)
	}
	inTwo, err := ops.Create(two, "In two", "b")
	if err != nil {
		t.Fatal(err)
	}
	cs, _ := mcpClientWatching(t, isolatedSet(t, oneDir, twoDir), 20*time.Millisecond)

	answered := make(chan watchOut, 1)
	go func() {
		var out watchOut
		call(t, cs, "issue_watch", map[string]any{"repo": []string{"one"}, "timeout": 10}, &out)
		answered <- out
	}()
	time.Sleep(100 * time.Millisecond)
	if _, _, err := ops.Comment(two, inTwo.ID, "elsewhere"); err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-answered:
		t.Fatalf("a change in two answered a watch on one: %+v", out.Changes)
	case <-time.After(300 * time.Millisecond):
	}
	if _, _, err := ops.Comment(one, inOne.ID, "here"); err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-answered:
		if len(out.Changes) != 1 || out.Changes[0].ID != inOne.ID || out.Changes[0].Repo != "one" {
			t.Errorf("answered %+v, want only %s in one", out.Changes, inOne.ID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a change in one did not answer a watch on one")
	}

	if msg := refused(t, cs, "issue_watch", map[string]any{"repo": []string{"three"}, "timeout": 1}); !strings.Contains(msg, "three") {
		t.Errorf("refusal %q", msg)
	}
}

// remotePair is a bare origin and two clones of it: here, which a watch
// watches, and there, a teammate's.
func remotePair(t *testing.T) (origin, here, there string) {
	t.Helper()
	origin = t.TempDir()
	run(t, "", "init", "-q", "--bare", origin)
	here, there = repoDir(t, "here"), repoDir(t, "there")
	run(t, here, "remote", "add", "origin", origin)
	run(t, there, "remote", "add", "origin", origin)
	return origin, here, there
}

func push(t *testing.T, st issuelib.Store) {
	t.Helper()
	if _, err := ops.Push(st, "origin", "", false, false); err != nil {
		t.Fatal(err)
	}
}

// TestWatchStore_Pulls: a watch that pulls hears what a teammate pushed --
// the pull's note, then the change it brought -- says a remote it cannot
// reach once, not every pull, and says when it is reached again. What the
// first pull brings is where the watch begins.
func TestWatchStore_Pulls(t *testing.T) {
	_, hereDir, thereDir := remotePair(t)
	here := issuelib.NewGitStoreAt(hereDir, &strings.Builder{})
	there := issuelib.NewGitStoreAt(thereDir, &strings.Builder{})
	issue, err := ops.Create(there, "Theirs", "b")
	if err != nil {
		t.Fatal(err)
	}
	push(t, there)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes, notes := make(chan watchChange, 16), make(chan pullNote, 16)
	go watchStore(ctx, here, watchFilter{}, 10*time.Millisecond, newPuller(here, "", "origin"), 50*time.Millisecond,
		func(ch watchChange) { changes <- ch }, func(n pullNote) { notes <- n })

	next := func(what string) pullNote {
		t.Helper()
		select {
		case n := <-notes:
			return n
		case <-time.After(5 * time.Second):
			t.Fatalf("no pull note: want %s", what)
		}
		return pullNote{}
	}
	if n := next("the first pull taking the issue"); n.ID != issue.ID {
		t.Fatalf("first pull: %+v", n)
	}
	select {
	case ch := <-changes:
		t.Fatalf("what the first pull brought was said as a change: %+v", ch)
	case <-time.After(200 * time.Millisecond):
	}

	if _, _, err := ops.Comment(there, issue.ID, "from there"); err != nil {
		t.Fatal(err)
	}
	push(t, there)
	if n := next("the comment taken"); n.ID != issue.ID || n.Remote != "origin" {
		t.Errorf("pull note %+v", n)
	}
	select {
	case ch := <-changes:
		if ch.ID != issue.ID || !hasPrefixed(ch.What, "comment") {
			t.Errorf("change %+v, want the comment", ch)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the pulled comment was not said")
	}

	run(t, hereDir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone"))
	if n := next("could not pull"); !strings.HasPrefix(n.What, couldNot) {
		t.Errorf("unreachable: %+v", n)
	}
	select {
	case n := <-notes:
		t.Errorf("an unchanged failure was said again: %+v", n)
	case <-time.After(300 * time.Millisecond):
	}
	run(t, hereDir, "remote", "set-url", "origin", originOf(t, thereDir))
	if n := next("reachable again"); n.What != "reachable again" {
		t.Errorf("reached again: %+v", n)
	}
}

func originOf(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(run(t, dir, "remote", "get-url", "origin"))
}

// TestMCP_IssueWatchRemote: issue_watch_remote pulls while it waits, and
// answers a teammate's pushed comment with the pull's note and the change.
func TestMCP_IssueWatchRemote(t *testing.T) {
	_, hereDir, thereDir := remotePair(t)
	here := issuelib.NewGitStoreAt(hereDir, &strings.Builder{})
	there := issuelib.NewGitStoreAt(thereDir, &strings.Builder{})
	issue, err := ops.Create(there, "Theirs", "b")
	if err != nil {
		t.Fatal(err)
	}
	push(t, there)
	if _, err := ops.Pull(here, "origin", false, false); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	serverT, clientT := mcp.NewInMemoryTransports()
	m := newMCPServer(isolatedSet(t, hereDir))
	m.fetch = 50 * time.Millisecond
	go m.s.Run(ctx, serverT)
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })

	// A remote it cannot reach answers on its own, once.
	good := originOf(t, hereDir)
	run(t, hereDir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone"))
	var down watchOut
	call(t, cs, "issue_watch_remote", map[string]any{"timeout": 10}, &down)
	if len(down.Pulls) != 1 || !strings.HasPrefix(down.Pulls[0].What, couldNot) {
		t.Fatalf("unreachable: %+v", down)
	}
	var still watchOut
	call(t, cs, "issue_watch_remote", map[string]any{"since": down.Cursor, "timeout": 1}, &still)
	if len(still.Pulls) != 0 || len(still.Changes) != 0 {
		t.Errorf("an unchanged failure answered again: %+v", still)
	}
	run(t, hereDir, "remote", "set-url", "origin", good)

	answered := make(chan watchOut, 1)
	go func() {
		var out watchOut
		call(t, cs, "issue_watch_remote", map[string]any{"ids": []string{issue.ID}, "timeout": 10}, &out)
		answered <- out
	}()
	time.Sleep(150 * time.Millisecond)
	if _, _, err := ops.Comment(there, issue.ID, "from there"); err != nil {
		t.Fatal(err)
	}
	push(t, there)
	select {
	case out := <-answered:
		if len(out.Changes) != 1 || !hasPrefixed(out.Changes[0].What, "comment") {
			t.Errorf("changes %+v, want the comment", out.Changes)
		}
		if !pulled(out.Pulls, issue.ID) {
			t.Errorf("pulls %+v, want the pull that took it", out.Pulls)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("issue_watch_remote did not answer the pushed comment")
	}
}

func pulled(notes []pullNote, id string) bool {
	for _, n := range notes {
		if n.ID == id {
			return true
		}
	}
	return false
}
