package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os/signal"
	"path"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

// serve exists for one reason: git-issue has no way to hand someone a link to an
// issue. Everything else it does, it does well from a terminal, so this is a
// viewer and nothing more.
//
// Read-only is a design constraint here, not a first cut. Sync is force-refspecs
// in both directions (see push.go and pull.go) and nothing in this codebase
// merges issue refs, so a second writer racing the CLI would silently drop
// whichever update lost. Closing that hole means real merge semantics for issue
// refs, which is a much larger piece of work. Until that exists there are no
// write endpoints and no "just close it from here" button.

// serveDefaultAddr binds loopback on purpose. Nothing served here authenticates
// anything, and it should stay that way; --addr is there for the person who
// knows what they are doing, not as an invitation.
const serveDefaultAddr = "localhost:8080"

type serveConfig struct {
	*cli.Command
	store issuelib.Store
	Addr  string `cli:"name=addr desc='address to listen on (default localhost:8080)'"`
}

// ServeCommand returns the serve subcommand.
func ServeCommand(store issuelib.Store) *cli.Command {
	cfg := &serveConfig{store: store}
	opts, _ := cli.StructOpts(cfg)
	return cli.NewCommandAt(&cfg.Command, "serve").
		WithSynopsis("serve [--addr <addr>] - Read-only web view of issues").
		WithOpts(opts...).
		WithRun(cfg.run)
}

func (cfg *serveConfig) run(cc *cli.Context, args []string) error {
	if _, err := cfg.Parse(cc, args); err != nil {
		return err
	}

	addr := cfg.Addr
	if addr == "" {
		addr = serveDefaultAddr
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	srv := &http.Server{
		Handler:           newIssueServer(cfg.store),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// cli hands us context.Background(), so nothing would ever cancel Serve.
	// Take over ctrl-c ourselves so in-flight responses finish instead of
	// being cut off mid-write.
	ctx, stop := signal.NotifyContext(cc.Go, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(cc.Out, "git-issue: read-only view at http://%s/ (ctrl-c to stop)\n", ln.Addr())

	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	<-done
	return nil
}

// issueServer answers the three questions a pasted link can ask: what issues are
// there, what does this one say, and what is in this attachment.
type issueServer struct {
	store issuelib.Store
	mux   *http.ServeMux
	cache *pageCache
}

func newIssueServer(store issuelib.Store) *issueServer {
	s := &issueServer{
		store: store,
		mux:   http.NewServeMux(),
		cache: newPageCache(),
	}
	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.HandleFunc("GET /i/{id}", s.handleIssue)
	s.mux.HandleFunc("GET /i/{id}/files/{path...}", s.handleAttachment)
	return s
}

func (s *issueServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// pageCache memoizes rendered bytes against a validity token. Every Store call
// forks git, so a cold page load costs a process per file it reads and several
// more per related issue it resolves. A ref's commit SHA changes on any change
// to that issue's content, which makes it both the cache token and an honest
// ETag; the index, which has no single ref, uses a digest over every ref and the
// commit it points at.
//
// The map is unbounded but holds one small entry per issue plus two for the
// index, which is the same order as the ref list the process already walks.
type pageCache struct {
	mu    sync.Mutex
	pages map[string]cachedPage
}

type cachedPage struct {
	token string
	body  []byte
}

func newPageCache() *pageCache {
	return &pageCache{pages: make(map[string]cachedPage)}
}

func (c *pageCache) get(key, token string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.pages[key]
	if !ok || p.token != token {
		return nil, false
	}
	return p.body, true
}

func (c *pageCache) put(key, token string, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pages[key] = cachedPage{token: token, body: body}
}

// serveHTML writes body as an HTML page carrying token as its ETag, answering
// 304 when the client already has that version. Cache-Control is no-cache
// rather than a max-age: the point is a cheap revalidation, not a stale page
// after someone comments.
func serveHTML(w http.ResponseWriter, r *http.Request, token string, body []byte) {
	etag := `"` + token + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if match := r.Header.Get("If-None-Match"); match != "" && etagMatches(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(body)
}

// etagMatches reports whether an If-None-Match header lists etag. Browsers send
// a comma-separated list and may weaken entries with a W/ prefix.
func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

func (s *issueServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Has("all")

	refs, err := s.store.ListRefs(all)
	if err != nil {
		s.errorPage(w, r, http.StatusInternalServerError, "failed to list issues: "+err.Error())
		return
	}
	sort.Strings(refs)

	// One GetRefCommit per ref is the cheapest complete statement of "has
	// anything changed"; the digest over all of them is the index's ETag.
	digest := sha256.New()
	live := make([]string, 0, len(refs))
	for _, ref := range refs {
		commit, err := s.store.GetRefCommit(ref)
		if err != nil {
			continue // a ref that vanished between listing and reading
		}
		fmt.Fprintf(digest, "%s %s\n", ref, commit)
		live = append(live, ref)
	}
	token := hex.EncodeToString(digest.Sum(nil))

	key := "index"
	if all {
		key = "index?all"
	}
	if body, ok := s.cache.get(key, token); ok {
		serveHTML(w, r, token, body)
		return
	}

	page := &indexPage{All: all}
	for _, ref := range live {
		issue, _, err := s.store.GetByRef(ref)
		if err != nil {
			continue
		}
		status := issuelib.StatusFromRef(ref)
		if status == "open" {
			page.OpenCount++
		} else {
			page.ClosedCount++
		}
		page.Issues = append(page.Issues, indexRow{
			ID:      issue.ID,
			Title:   issue.Title,
			Status:  status,
			Labels:  issue.Labels,
			Updated: issue.Updated,
		})
	}
	// Newest first, matching what `git issue list` prints.
	sort.SliceStable(page.Issues, func(i, j int) bool {
		return page.Issues[i].Updated.After(page.Issues[j].Updated)
	})

	body, err := renderPage("index", page)
	if err != nil {
		s.errorPage(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.cache.put(key, token, body)
	serveHTML(w, r, token, body)
}

func (s *issueServer) handleIssue(w http.ResponseWriter, r *http.Request) {
	ref, xidr, ok := s.resolve(w, r, r.PathValue("id"), "")
	if !ok {
		return
	}

	commit, err := s.store.GetRefCommit(ref)
	if err != nil {
		s.errorPage(w, r, http.StatusNotFound, "issue not found")
		return
	}
	// Status lives in the ref namespace, and closing an issue moves the ref
	// without rewriting the commit -- so the commit alone is not a complete
	// statement of what this page says. Pair it with the status it was
	// rendered under.
	token := commit + "." + issuelib.StatusFromRef(ref)
	key := "issue/" + xidr
	if body, ok := s.cache.get(key, token); ok {
		serveHTML(w, r, token, body)
		return
	}

	page, err := s.buildIssuePage(ref, xidr)
	if err != nil {
		s.errorPage(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	body, err := renderPage("issue", page)
	if err != nil {
		s.errorPage(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.cache.put(key, token, body)
	serveHTML(w, r, token, body)
}

func (s *issueServer) buildIssuePage(ref, xidr string) (*issuePage, error) {
	issue, desc, err := s.store.GetByRef(ref)
	if err != nil {
		return nil, fmt.Errorf("failed to read issue: %w", err)
	}

	page := &issuePage{
		ID:      issuelib.FormatID(issue.ID),
		Title:   issue.Title,
		Status:  issuelib.StatusFromRef(ref),
		Ref:     ref,
		Created: issue.Created,
		Updated: issue.Updated,
		Labels:  issue.Labels,
		// The title is already the page heading; repeating description.md's
		// leading "# ..." underneath it just prints it twice.
		Description: renderMarkdown(stripTitleLine(desc)),
		Branches:    issue.Branches,
	}

	for _, commit := range issue.Commits {
		info, err := s.store.GetCommitInfo(commit)
		if err != nil || info == "" {
			info = commit
		}
		page.Commits = append(page.Commits, info)
	}

	// Same relations, in the same order, as `git issue show` prints.
	page.Relations = []linkSection{
		{Title: "Related issues", Links: s.resolveLinks(issue.RelatedIssues)},
		{Title: "Blocks", Links: s.resolveLinks(issue.Blocks)},
		{Title: "Blocked by", Links: s.resolveLinks(issue.BlockedBy)},
		{Title: "Duplicates", Links: s.resolveLinks(issue.Duplicates)},
	}

	comments, attachments := s.walkDiscussionTree(ref)
	page.Comments = s.readComments(ref, comments)
	for _, p := range attachments {
		rel := strings.TrimPrefix(p, attachmentRoot+"/")
		page.Attachments = append(page.Attachments, attachmentRow{
			Name: rel,
			Href: "/i/" + xidr + "/files/" + escapePathSegments(rel),
		})
	}
	sort.Slice(page.Attachments, func(i, j int) bool {
		return page.Attachments[i].Name < page.Attachments[j].Name
	})

	return page, nil
}

// resolveLinks turns the XIDRs in meta.tony into rows that carry the linked
// issue's title and current status, so the reader does not have to open each one
// to find out what it is. A dangling link renders as itself rather than
// disappearing.
func (s *issueServer) resolveLinks(xidrs []string) []issueLink {
	var links []issueLink
	for _, xidr := range xidrs {
		link := issueLink{ID: issuelib.FormatID(xidr), Href: "/i/" + xidr}
		ref, err := s.store.FindRef(xidr)
		if err != nil {
			links = append(links, link)
			continue
		}
		issue, _, err := s.store.GetByRef(ref)
		if err != nil {
			links = append(links, link)
			continue
		}
		link.Found = true
		link.Title = issue.Title
		link.Status = issuelib.StatusFromRef(ref)
		links = append(links, link)
	}
	return links
}

// readComments loads each comment and orders it by the timestamp in its header,
// for the same reason show.go does: the filenames are content-addressed and do
// not sort chronologically.
func (s *issueServer) readComments(ref string, paths []string) []commentRow {
	rows := make([]commentRow, 0, len(paths))
	for _, p := range paths {
		raw, err := s.store.ReadFile(ref, p)
		if err != nil {
			continue
		}
		content := string(raw)
		ts, hasTS := parseCommentTime(content)
		rows = append(rows, commentRow{
			Path:  p,
			When:  ts,
			HasTS: hasTS,
			Body:  renderMarkdown(stripCommentHeader(content)),
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].HasTS && rows[j].HasTS && !rows[i].When.Equal(rows[j].When) {
			return rows[i].When.Before(rows[j].When)
		}
		return rows[i].Path < rows[j].Path
	})
	return rows
}

// attachmentRoot is where `git issue attach` puts files inside an issue tree.
const attachmentRoot = "discussion/files"

// walkDiscussionTree splits the discussion subtree into comments and
// attachments, using the same rule as the terminal view: an .md outside
// discussion/files/ is a comment, anything else is an attachment.
func (s *issueServer) walkDiscussionTree(ref string) (comments, attachments []string) {
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := s.store.ListDir(ref, dir)
		if err != nil {
			return
		}
		for name, entry := range entries {
			full := dir + "/" + name
			switch {
			case strings.HasPrefix(entry, "tree:"):
				walk(full)
			case isCommentFile(full):
				comments = append(comments, full)
			default:
				attachments = append(attachments, full)
			}
		}
	}
	walk("discussion")
	return comments, attachments
}

func (s *issueServer) handleAttachment(w http.ResponseWriter, r *http.Request) {
	rel := path.Clean("/" + r.PathValue("path"))
	if rel == "/" {
		s.errorPage(w, r, http.StatusNotFound, "no such attachment")
		return
	}
	// Cleaning against "/" resolves any ".." the client sent; what is left is
	// always inside the attachment root.
	ref, _, ok := s.resolve(w, r, r.PathValue("id"), "/files"+escapePathSegments(strings.TrimPrefix(rel, "/")))
	if !ok {
		return
	}
	treePath := attachmentRoot + rel

	// `git show ref:some/dir` prints a tree listing rather than failing, so
	// confirm the entry is a blob before reading it.
	dir, name := path.Split(treePath)
	entries, err := s.store.ListDir(ref, strings.TrimSuffix(dir, "/"))
	if err != nil {
		s.errorPage(w, r, http.StatusNotFound, "no such attachment")
		return
	}
	if entry, ok := entries[name]; !ok || !strings.HasPrefix(entry, "blob:") {
		s.errorPage(w, r, http.StatusNotFound, "no such attachment")
		return
	}

	data, err := s.store.ReadFile(ref, treePath)
	if err != nil {
		s.errorPage(w, r, http.StatusNotFound, "no such attachment")
		return
	}

	// Attachment bytes are whatever someone handed to `git issue attach`, and
	// they are served from the same origin as the issue pages. An attached
	// .html rendered in place would be stored XSS on that origin, so nothing
	// here is ever rendered: one opaque type, a download disposition, and
	// nosniff so the browser does not second-guess any of it.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", contentDisposition(name))
	if commit, err := s.store.GetRefCommit(ref); err == nil {
		w.Header().Set("ETag", `"`+commit+`"`)
		w.Header().Set("Cache-Control", "no-cache")
		if match := r.Header.Get("If-None-Match"); match != "" && etagMatches(match, `"`+commit+`"`) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	_, _ = w.Write(data)
}

// resolve maps the {id} segment of a URL onto an issue ref.
//
// Links get pasted into chat and must not rot, which drives both halves of this.
// FindRef searches refs/issues/ and refs/closed/ together, so a link keeps
// working after the issue closes and its ref moves namespace. And a prefix is
// redirected to the full XIDR rather than served, so the URL that gets copied
// back out of the address bar is the one that stays unambiguous as more issues
// are created. suffix is whatever follows the id in the canonical path.
func (s *issueServer) resolve(w http.ResponseWriter, r *http.Request, id, suffix string) (ref, xidr string, ok bool) {
	if id == "" {
		s.errorPage(w, r, http.StatusNotFound, "no issue id in URL")
		return "", "", false
	}
	ref, err := s.store.FindRef(id)
	if err != nil {
		s.errorPage(w, r, http.StatusNotFound, err.Error())
		return "", "", false
	}
	xidr, err = issuelib.XIDRFromRef(ref)
	if err != nil {
		s.errorPage(w, r, http.StatusInternalServerError, err.Error())
		return "", "", false
	}
	if id != xidr {
		target := "/i/" + xidr + suffix
		if q := r.URL.RawQuery; q != "" {
			target += "?" + q
		}
		// Found, not a permanent redirect: the target is stable, but a prefix
		// that is unique today may not be tomorrow, and that answer should not
		// be cached forever.
		http.Redirect(w, r, target, http.StatusFound)
		return "", "", false
	}
	return ref, xidr, true
}

func (s *issueServer) errorPage(w http.ResponseWriter, r *http.Request, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	body, err := renderPage("error", &errorPageData{Status: status, Message: msg})
	if err != nil {
		fmt.Fprintf(w, "%d\n", status)
		return
	}
	_, _ = w.Write(body)
}
