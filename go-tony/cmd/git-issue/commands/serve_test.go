package commands

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

// GitStore drives the git binary in the process's working directory, so a test
// repo is a temp dir plus t.Chdir. That makes these tests serial by nature --
// they must not call t.Parallel().
func testRepo(t *testing.T) issuelib.Store {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"commit", "-q", "--allow-empty", "-m", "root"},
	} {
		cmd := exec.Command("git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return issuelib.NewGitStoreWithOutput(&strings.Builder{})
}

// addComment writes a comment file directly, so a test can pick the exact
// header form it wants to exercise.
func addComment(t *testing.T, store issuelib.Store, issue *issuelib.Issue, path, content string) {
	t.Helper()
	if err := store.Update(issue, "comment", map[string]string{path: content}); err != nil {
		t.Fatalf("failed to add comment %s: %v", path, err)
	}
}

func get(t *testing.T, srv http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// TestServe_PrefixRedirectsToCanonicalURL: a pasted link must end up spelled the
// way it will still resolve once more issues exist.
func TestServe_PrefixRedirectsToCanonicalURL(t *testing.T) {
	store := testRepo(t)
	issue, err := store.Create("Prefix", "# Prefix\n\nbody\n")
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	srv := newIssueServer(store)

	cases := []struct {
		name, path, wantLocation string
		wantStatus               int
	}{
		{"prefix", "/i/" + issue.ID[:6], "/i/" + issue.ID, http.StatusFound},
		{"prefix with query", "/i/" + issue.ID[:6] + "?x=1", "/i/" + issue.ID + "?x=1", http.StatusFound},
		{"canonical", "/i/" + issue.ID, "", http.StatusOK},
		{"unknown", "/i/zzzzzzzz", "", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := get(t, srv, tc.path)
			if rec.Code != tc.wantStatus {
				t.Fatalf("GET %s: status %d, want %d", tc.path, rec.Code, tc.wantStatus)
			}
			if got := rec.Header().Get("Location"); got != tc.wantLocation {
				t.Fatalf("GET %s: Location %q, want %q", tc.path, got, tc.wantLocation)
			}
		})
	}
}

// TestServe_ResolvesAfterClose is the whole point of routing through FindRef:
// closing an issue moves its ref to another namespace, and the link must not
// notice.
func TestServe_ResolvesAfterClose(t *testing.T) {
	store := testRepo(t)
	issue, err := store.Create("Closes", "# Closes\n\nstill reachable\n")
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	srv := newIssueServer(store)

	if rec := get(t, srv, "/i/"+issue.ID); rec.Code != http.StatusOK {
		t.Fatalf("before close: status %d, want 200", rec.Code)
	}

	if err := store.MoveRef(issuelib.RefForXIDR(issue.ID), issuelib.ClosedRefForXIDR(issue.ID)); err != nil {
		t.Fatalf("failed to close issue: %v", err)
	}

	rec := get(t, srv, "/i/"+issue.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("after close: status %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "still reachable") {
		t.Fatalf("after close: description missing from page:\n%s", body)
	}
	if !strings.Contains(body, `class="status closed"`) {
		t.Fatalf("after close: page does not show the issue as closed:\n%s", body)
	}

	// The prefix form has to follow the ref across namespaces too.
	rec = get(t, srv, "/i/"+issue.ID[:6])
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/i/"+issue.ID {
		t.Fatalf("after close: prefix gave %d -> %q", rec.Code, rec.Header().Get("Location"))
	}

	// A closed issue drops off the default list but not off /?all=1.
	if body := get(t, srv, "/").Body.String(); strings.Contains(body, issue.ID) {
		t.Fatalf("closed issue still listed on /:\n%s", body)
	}
	if body := get(t, srv, "/?all=1").Body.String(); !strings.Contains(body, issue.ID) {
		t.Fatalf("closed issue missing from /?all=1:\n%s", body)
	}
}

// TestServe_CommentHeaderForms covers both header spellings parseCommentTime
// accepts, and checks the header itself does not leak into the rendered body.
func TestServe_CommentHeaderForms(t *testing.T) {
	store := testRepo(t)
	issue, err := store.Create("Comments", "# Comments\n\nbody\n")
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	cases := []struct {
		name, path, content, wantText string
		wantTime                      time.Time
	}{
		{
			name:     "legacy header",
			path:     "discussion/20260726T003417Z-aaaa2222.md",
			content:  "<!-- Comment 003 - 2026-07-26T02:34:17+02:00 -->\n\nlegacy body\n",
			wantText: "legacy body",
			wantTime: time.Date(2026, 7, 26, 2, 34, 17, 0, time.FixedZone("", 2*3600)),
		},
		{
			name:     "current header",
			path:     "discussion/20260726T143259Z-ffff0000.md",
			content:  "<!-- 2026-07-26T16:32:59+02:00 -->\n\ncurrent body\n",
			wantText: "current body",
			wantTime: time.Date(2026, 7, 26, 16, 32, 59, 0, time.FixedZone("", 2*3600)),
		},
	}
	for _, tc := range cases {
		addComment(t, store, issue, tc.path, tc.content)
	}

	body := get(t, newIssueServer(store), "/i/"+issue.ID).Body.String()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(body, tc.wantText) {
				t.Fatalf("comment body %q missing from page:\n%s", tc.wantText, body)
			}
			want := tc.wantTime.Local().Format("2006-01-02 15:04 MST")
			if !strings.Contains(body, want) {
				t.Fatalf("comment timestamp %q missing from page:\n%s", want, body)
			}
			// The header is stripped before rendering; with raw HTML disabled
			// it would otherwise surface as goldmark's placeholder.
			if strings.Contains(body, "raw HTML omitted") {
				t.Fatalf("comment header reached the renderer:\n%s", body)
			}
		})
	}

	// Chronological order, which the content-addressed filenames do not give.
	if i, j := strings.Index(body, "legacy body"), strings.Index(body, "current body"); i > j {
		t.Fatalf("comments out of chronological order:\n%s", body)
	}
}

// TestServe_RawHTMLDoesNotSurvive: issue text is untrusted-ish and is served
// from the same origin as everything else, so no markup in it may reach the
// page as markup.
func TestServe_RawHTMLDoesNotSurvive(t *testing.T) {
	const payload = `# Injected

<script>alert('desc')</script>

<img src=x onerror="alert('img')">

Inline <b onclick="alert(1)">markup</b> and a [bad link](javascript:alert('link')).
`
	store := testRepo(t)
	issue, err := store.Create("Injected", payload)
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	addComment(t, store, issue, "discussion/20260726T003417Z-deadbeef.md",
		"<!-- 2026-07-26T02:34:17+02:00 -->\n\n<script>alert('comment')</script>\n")

	body := get(t, newIssueServer(store), "/i/"+issue.ID).Body.String()
	for _, bad := range []string{
		"<script>",
		"onerror=",
		"onclick=",
		`href="javascript:`,
	} {
		if strings.Contains(body, bad) {
			t.Fatalf("raw HTML %q survived into the response:\n%s", bad, body)
		}
	}
	// The text is still there, just inert.
	if !strings.Contains(body, "Inline") || !strings.Contains(body, "markup") {
		t.Fatalf("markdown body was dropped rather than escaped:\n%s", body)
	}
}

// TestServe_TitleInjection guards the parts of a page that are not markdown:
// the title comes out of description.md's first line and goes through
// html/template, which must escape it.
func TestServe_TitleInjection(t *testing.T) {
	store := testRepo(t)
	issue, err := store.Create("x", "# <script>alert(1)</script>\n\nbody\n")
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	srv := newIssueServer(store)
	for _, target := range []string{"/", "/i/" + issue.ID} {
		body := get(t, srv, target).Body.String()
		if strings.Contains(body, "<script>alert(1)</script>") {
			t.Fatalf("GET %s: unescaped title in response:\n%s", target, body)
		}
		if !strings.Contains(body, "&lt;script&gt;") {
			t.Fatalf("GET %s: escaped title missing from response:\n%s", target, body)
		}
	}
}

// TestServe_AttachmentsAreNeverRendered: an attached .html served inline would
// be stored XSS on this origin, so attachments always download as opaque bytes.
func TestServe_AttachmentsAreNeverRendered(t *testing.T) {
	store := testRepo(t)
	issue, err := store.Create("Attached", "# Attached\n\nbody\n")
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	const evil = "<script>alert('xss')</script>"
	if err := store.Update(issue, "attach", map[string]string{
		attachmentRoot + "/evil.html":      evil,
		attachmentRoot + "/notes/deep.txt": "deep\n",
	}); err != nil {
		t.Fatalf("failed to attach: %v", err)
	}
	srv := newIssueServer(store)

	// The issue page links attachments but does not inline them.
	page := get(t, srv, "/i/"+issue.ID).Body.String()
	if strings.Contains(page, "<script>alert('xss')</script>") {
		t.Fatalf("attachment content inlined into the issue page:\n%s", page)
	}
	for _, want := range []string{"/i/" + issue.ID + "/files/evil.html", "/i/" + issue.ID + "/files/notes/deep.txt"} {
		if !strings.Contains(page, want) {
			t.Fatalf("attachment link %q missing from page:\n%s", want, page)
		}
	}

	rec := get(t, srv, "/i/"+issue.ID+"/files/evil.html")
	if rec.Code != http.StatusOK {
		t.Fatalf("attachment: status %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("attachment Content-Type %q, want application/octet-stream", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("attachment Content-Disposition %q, want an attachment disposition", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("attachment X-Content-Type-Options %q, want nosniff", got)
	}
	if rec.Body.String() != evil {
		t.Fatalf("attachment bytes altered: %q", rec.Body.String())
	}

	// Nested paths resolve; directories, missing names and traversal do not.
	if rec := get(t, srv, "/i/"+issue.ID+"/files/notes/deep.txt"); rec.Body.String() != "deep\n" {
		t.Fatalf("nested attachment: got %q", rec.Body.String())
	}
	for _, bad := range []string{
		"/i/" + issue.ID + "/files/notes",
		"/i/" + issue.ID + "/files/missing.txt",
		// Percent-encoded so the traversal reaches the handler rather than
		// being cleaned away by the mux.
		"/i/" + issue.ID + "/files/%2e%2e/%2e%2e/meta.tony",
	} {
		if rec := get(t, srv, bad); rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s: status %d, want 404 (body %q)", bad, rec.Code, rec.Body.String())
		}
	}
	// The un-encoded form is cleaned by the mux before it ever reaches the
	// attachment handler; what matters is that it does not yield meta.tony.
	if rec := get(t, srv, "/i/"+issue.ID+"/files/../../meta.tony"); rec.Code == http.StatusOK {
		t.Fatalf("path traversal served content: %q", rec.Body.String())
	}
}

// TestServe_ETagRevalidation: GetRefCommit is the cache token, so an unchanged
// issue answers 304 and a changed one does not.
func TestServe_ETagRevalidation(t *testing.T) {
	store := testRepo(t)
	issue, err := store.Create("Caching", "# Caching\n\nbody\n")
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	srv := newIssueServer(store)

	first := get(t, srv, "/i/"+issue.ID)
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on the issue page")
	}

	conditional := func(target, etag string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("If-None-Match", etag)
		srv.ServeHTTP(rec, req)
		return rec
	}

	if rec := conditional("/i/"+issue.ID, etag); rec.Code != http.StatusNotModified {
		t.Fatalf("unchanged issue: status %d, want 304", rec.Code)
	}

	addComment(t, store, issue, "discussion/20260726T003417Z-cafe0000.md",
		"<!-- 2026-07-26T02:34:17+02:00 -->\n\nnew comment\n")

	rec := conditional("/i/"+issue.ID, etag)
	if rec.Code != http.StatusOK {
		t.Fatalf("changed issue: status %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "new comment") {
		t.Fatalf("changed issue served stale content:\n%s", rec.Body.String())
	}
	if rec.Header().Get("ETag") == etag {
		t.Fatal("ETag did not change after the issue changed")
	}

	// The index revalidates on the aggregate of every ref's commit.
	index := get(t, srv, "/")
	indexETag := index.Header().Get("ETag")
	if indexETag == "" {
		t.Fatal("no ETag on the index")
	}
	if rec := conditional("/", indexETag); rec.Code != http.StatusNotModified {
		t.Fatalf("unchanged index: status %d, want 304", rec.Code)
	}
	if _, err := store.Create("Second", "# Second\n\nbody\n"); err != nil {
		t.Fatalf("failed to create second issue: %v", err)
	}
	if rec := conditional("/", indexETag); rec.Code != http.StatusOK {
		t.Fatalf("index after a new issue: status %d, want 200", rec.Code)
	}
}

// TestServe_IssueContentInventory checks the web view shows what `git issue
// show` shows: labels, links, relations and attachments, not just the body.
func TestServe_IssueContentInventory(t *testing.T) {
	store := testRepo(t)
	other, err := store.Create("Other", "# Other\n\nother body\n")
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	issue, err := store.Create("Main", "# Main\n\nmain body\n")
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	head, err := store.VerifyCommit("HEAD")
	if err != nil {
		t.Fatalf("failed to resolve HEAD: %v", err)
	}
	issue.Labels = []string{"design", "web"}
	issue.Branches = []string{"feature/serve"}
	issue.Commits = []string{head}
	issue.RelatedIssues = []string{other.ID}
	issue.Blocks = []string{other.ID}
	issue.BlockedBy = []string{"nosuchissue0000000000"}
	issue.Duplicates = []string{other.ID}
	if err := store.Update(issue, "meta", nil); err != nil {
		t.Fatalf("failed to update issue: %v", err)
	}

	body := get(t, newIssueServer(store), "/i/"+issue.ID).Body.String()
	for _, want := range []string{
		"main body",
		"design", "web",
		"feature/serve",
		head[:7],
		"Related issues", "Blocks", "Blocked by", "Duplicates",
		"/i/" + other.ID,
		"Other",
		"(not found)",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("issue page missing %q:\n%s", want, body)
		}
	}
	// The description heading is not printed twice: once as <h1>, not again in
	// the rendered body.
	if n := strings.Count(body, ">Main<"); n != 1 {
		t.Fatalf("title rendered %d times, want 1:\n%s", n, body)
	}
}

func TestStripTitleLine(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"heading stripped", "# Title\n\nbody\n", "body\n"},
		{"no heading kept", "just text\nmore\n", "just text\nmore\n"},
		{"deeper heading kept", "## Title\n\nbody\n", "## Title\n\nbody\n"},
		{"single line", "# Title", "# Title"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripTitleLine(tc.in); got != tc.want {
				t.Fatalf("stripTitleLine(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestContentDisposition(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain", "notes.txt", `attachment; filename="notes.txt"`},
		{"quote", `a"b.txt`, `attachment; filename="a_b.txt"`},
		{"control chars dropped", "a\r\nX-Evil: 1.txt", `attachment; filename="aX-Evil: 1.txt"`},
		{"non-ascii", "café.txt", `attachment; filename="caf_.txt"; filename*=UTF-8''caf%C3%A9.txt`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := contentDisposition(tc.in); got != tc.want {
				t.Fatalf("contentDisposition(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
