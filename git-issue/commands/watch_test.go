package commands

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

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
	if len(next.Changes) != 1 || next.Changes[0].Status != "closed" || !hasPrefixed(next.Changes[0].What, "closed") {
		t.Fatalf("answered %+v, want the close", next.Changes)
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
		done <- watchStore(ctx, store, watchFilter{label: "bug"}, 10*time.Millisecond, func(ch watchChange) { got <- ch })
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
	if last := changes[len(changes)-1]; !hasPrefixed(last.What, "closed") {
		t.Errorf("last = %+v, want the move to closed", last)
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
	go watchStore(ctx, store, watchFilter{}, 10*time.Millisecond, func(ch watchChange) { got <- ch })
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
