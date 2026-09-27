package commands

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

// watchingServer is a served view of st that watches, pulling with p when
// there is one, and what a page that began when it did is told changed.
func watchingServer(t *testing.T, st issuelib.Store, p *puller) (*httptest.Server, func() changesOut) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	handler := newIssueServer(st)
	live := newLiveView(p)
	handler.watching(live)
	srv := httptest.NewServer(handler)
	t.Cleanup(func() {
		cancel()
		srv.Close()
	})
	go watchStore(ctx, st, watchFilter{}, 10*time.Millisecond, p, 50*time.Millisecond,
		live.changed, func(pullNote) {}, live.pulled)

	ask := func(cursor string) changesOut {
		t.Helper()
		res, err := http.Get(srv.URL + "/changes?since=" + url.QueryEscape(cursor))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out changesOut
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	cursor := ask("").Cursor
	return srv, func() changesOut {
		out := ask(cursor)
		cursor = out.Cursor
		return out
	}
}

func page(t *testing.T, srv *httptest.Server, target string) string {
	t.Helper()
	res, err := http.Get(srv.URL + target)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// hears asks what changed until it is told want.
func hears(t *testing.T, changes func() changesOut, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		out := changes()
		if out.Reload {
			t.Fatalf("told to reload, waiting for %q", want)
		}
		for _, got := range out.Changed {
			if got == want {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no change %q", want)
}

// TestServe_WithoutWatch: a server that does not watch serves no script, no
// events, and pages that say nothing of a watch.
func TestServe_WithoutWatch(t *testing.T) {
	store := testRepo(t)
	issue, err := ops.Create(store, "Plain", "b")
	if err != nil {
		t.Fatal(err)
	}
	srv := newIssueServer(store)
	for _, target := range []string{"/", "/i/" + issue.ID} {
		body := get(t, srv, target).Body.String()
		for _, not := range []string{"watch.js", "data-watch", "watching"} {
			if strings.Contains(body, not) {
				t.Errorf("%s says %q without -watch", target, not)
			}
		}
	}
	for _, target := range []string{"/watch.js", "/changes"} {
		if code := get(t, srv, target).Code; code != http.StatusNotFound {
			t.Errorf("%s answered %d without -watch", target, code)
		}
	}
}

// TestServe_Watch: a watching server's pages carry the script and the issue
// they reload for, and what changed names an issue changed beside the
// server; the page then served says the change. A cursor that is not this
// server's is told to reload.
func TestServe_Watch(t *testing.T) {
	dir := repoDir(t, "one")
	store := issuelib.NewGitStoreAt(dir, &strings.Builder{})
	issue, err := ops.Create(store, "Watched", "b")
	if err != nil {
		t.Fatal(err)
	}
	srv, changes := watchingServer(t, store, nil)

	if body := page(t, srv, "/i/"+issue.ID); !strings.Contains(body, `<script src="/watch.js" defer></script>`) ||
		!strings.Contains(body, `data-watch="`+issue.ID+`"`) {
		t.Errorf("the issue's page does not watch its issue:\n%s", body)
	}
	if body := page(t, srv, "/"); !strings.Contains(body, `data-watch=""`) {
		t.Errorf("the index does not watch every issue:\n%s", body)
	}
	if js := page(t, srv, "/watch.js"); !strings.Contains(js, "/changes") {
		t.Errorf("/watch.js: %q", js)
	}
	for _, cursor := range []string{"other.3", "garbage"} {
		if body := page(t, srv, "/changes?since="+cursor); !strings.Contains(body, `"reload":true`) {
			t.Errorf("a cursor %q was answered %s", cursor, body)
		}
	}

	time.Sleep(50 * time.Millisecond) // the watch has taken its first look
	if _, _, err := ops.Comment(store, issue.ID, "from a shell"); err != nil {
		t.Fatal(err)
	}
	hears(t, changes, issue.ID)
	if body := page(t, srv, "/i/"+issue.ID); !strings.Contains(body, "from a shell") {
		t.Errorf("the page after the event does not say the comment")
	}
}

// TestServe_WatchPulls: a watching server pulls, so a teammate's pushed
// comment reaches the page; a refusal is shown on the index and on the
// refused issue's page, not on another's, and every page is told when it
// begins and when it clears.
func TestServe_WatchPulls(t *testing.T) {
	_, hereDir, thereDir := remotePair(t)
	here := issuelib.NewGitStoreAt(hereDir, &strings.Builder{})
	there := issuelib.NewGitStoreAt(thereDir, &strings.Builder{})
	issue, err := ops.Create(there, "Theirs", "b")
	if err != nil {
		t.Fatal(err)
	}
	other, err := ops.Create(there, "Another", "b")
	if err != nil {
		t.Fatal(err)
	}
	push(t, there)
	if _, err := ops.Pull(here, "origin", false, false); err != nil {
		t.Fatal(err)
	}
	srv, changes := watchingServer(t, here, newPuller(here, hereDir, "", "origin", time.Minute))
	time.Sleep(100 * time.Millisecond)

	if _, _, err := ops.Comment(there, issue.ID, "from there"); err != nil {
		t.Fatal(err)
	}
	push(t, there)
	hears(t, changes, issue.ID)
	if body := page(t, srv, "/i/"+issue.ID); !strings.Contains(body, "from there") {
		t.Errorf("the page does not say the pulled comment")
	}

	contest(t, here, there, issue.ID)
	hears(t, changes, anyPage)
	for target, shown := range map[string]bool{"/": true, "/i/" + issue.ID: true, "/i/" + other.ID: false} {
		if body := page(t, srv, target); strings.Contains(body, "refused: ") != shown {
			t.Errorf("%s shows the refusal: %v, want %v", target, !shown, shown)
		}
	}

	if _, err := ops.Pull(here, "origin", true, false); err != nil {
		t.Fatal(err)
	}
	hears(t, changes, anyPage)
	if body := page(t, srv, "/"); strings.Contains(body, "refused: ") {
		t.Errorf("the index shows a refusal that was settled")
	}
}
