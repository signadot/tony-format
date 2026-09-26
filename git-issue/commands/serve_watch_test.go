package commands

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

// watchingServer is a served view of st that watches, pulling with p when
// there is one, and the events it sends a page.
func watchingServer(t *testing.T, st issuelib.Store, p *puller) (*httptest.Server, <-chan string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	handler := newIssueServer(st)
	live := newLiveView(p)
	handler.watching(live)
	srv := httptest.NewServer(handler)
	t.Cleanup(func() {
		cancel()
		live.close()
		srv.Close()
	})
	go watchStore(ctx, st, watchFilter{}, 10*time.Millisecond, p, 50*time.Millisecond,
		live.changed, func(pullNote) {}, live.pulled)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("/events is %q", ct)
	}
	events := make(chan string, 64)
	go func() {
		defer res.Body.Close()
		lines := bufio.NewScanner(res.Body)
		for lines.Scan() {
			if data, ok := strings.CutPrefix(lines.Text(), "data: "); ok {
				events <- data
			}
		}
	}()
	return srv, events
}

func page(t *testing.T, srv *httptest.Server, target string) string {
	t.Helper()
	res, err := http.Get(srv.URL + target)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var b strings.Builder
	lines := bufio.NewScanner(res.Body)
	lines.Buffer(make([]byte, 1<<20), 1<<20)
	for lines.Scan() {
		b.WriteString(lines.Text() + "\n")
	}
	return b.String()
}

func hears(t *testing.T, events <-chan string, want string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-events:
			if got == want {
				return
			}
		case <-deadline:
			t.Fatalf("no event %q", want)
		}
	}
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
	for _, target := range []string{"/watch.js", "/events"} {
		if code := get(t, srv, target).Code; code != http.StatusNotFound {
			t.Errorf("%s answered %d without -watch", target, code)
		}
	}
}

// TestServe_Watch: a watching server's pages carry the script and the issue
// they reload for, and the events name an issue changed beside the server;
// the page then served says the change.
func TestServe_Watch(t *testing.T) {
	dir := repoDir(t, "one")
	store := issuelib.NewGitStoreAt(dir, &strings.Builder{})
	issue, err := ops.Create(store, "Watched", "b")
	if err != nil {
		t.Fatal(err)
	}
	srv, events := watchingServer(t, store, nil)

	if body := page(t, srv, "/i/"+issue.ID); !strings.Contains(body, `<script src="/watch.js" defer></script>`) ||
		!strings.Contains(body, `data-watch="`+issue.ID+`"`) {
		t.Errorf("the issue's page does not watch its issue:\n%s", body)
	}
	if body := page(t, srv, "/"); !strings.Contains(body, `data-watch=""`) {
		t.Errorf("the index does not watch every issue:\n%s", body)
	}
	if js := page(t, srv, "/watch.js"); !strings.Contains(js, "EventSource") {
		t.Errorf("/watch.js: %q", js)
	}

	time.Sleep(50 * time.Millisecond) // the watch has taken its first look
	if _, _, err := ops.Comment(store, issue.ID, "from a shell"); err != nil {
		t.Fatal(err)
	}
	hears(t, events, issue.ID)
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
	srv, events := watchingServer(t, here, newPuller(here, hereDir, "", "origin", time.Minute))
	time.Sleep(100 * time.Millisecond)

	if _, _, err := ops.Comment(there, issue.ID, "from there"); err != nil {
		t.Fatal(err)
	}
	push(t, there)
	hears(t, events, issue.ID)
	if body := page(t, srv, "/i/"+issue.ID); !strings.Contains(body, "from there") {
		t.Errorf("the page does not say the pulled comment")
	}

	contest(t, here, there, issue.ID)
	hears(t, events, anyPage)
	for target, shown := range map[string]bool{"/": true, "/i/" + issue.ID: true, "/i/" + other.ID: false} {
		if body := page(t, srv, target); strings.Contains(body, "refused: ") != shown {
			t.Errorf("%s shows the refusal: %v, want %v", target, !shown, shown)
		}
	}

	if _, err := ops.Pull(here, "origin", true, false); err != nil {
		t.Fatal(err)
	}
	hears(t, events, anyPage)
	if body := page(t, srv, "/"); strings.Contains(body, "refused: ") {
		t.Errorf("the index shows a refusal that was settled")
	}
}
