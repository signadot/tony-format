package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

// Resources: what a host reads INTO context, as opposed to tools, which an
// agent acts through. An issue is issue://<xidr>, as a person reads it; its
// data is issue://<xidr>/meta; the open issues across the working set are
// issue://. A host that offers resources by pattern gets the templates, and
// completion on their id; one that subscribes hears a change.
//
// The SDK's listing is its registered resources, so one is registered per open
// issue -- at start, and whenever the set changes -- and the SDK sends
// resources/list_changed itself when that registration changes. A change to an
// issue the server made with its own tools is announced by the tool; a change
// made beside the server -- a comment from a shell, a pull in another clone --
// is found by the watch: a poll of every served repository's refs, tips
// compared between two looks, so "watching" is not a word (eg8zmb1sh12ksr48pxn0).

const (
	uriScheme     = "issue://"
	metaSuffix    = "/meta"
	mimeMarkdown  = "text/markdown"
	mimeJSON      = "application/json"
	mimeText      = "text/plain"
	defaultPoll   = 5 * time.Second
	completeLimit = 100
)

// mcpServer is the server, its working set, and what it has registered and
// last seen -- the state a watch needs, and nothing the repositories do not
// hold.
type mcpServer struct {
	s  *mcp.Server
	ws *workspace

	mu         sync.Mutex
	registered map[string]bool   // issue URIs registered as resources
	seen       map[string]string // ref -> commit, per repository dir: last look
}

func uriFor(xidr string) string { return uriScheme + xidr }

// parseURI answers the id a resource URI names, and whether it asks for the
// meta form. The root has no id.
func parseURI(uri string) (id string, meta bool, ok bool) {
	rest, ok := strings.CutPrefix(uri, uriScheme)
	if !ok {
		return "", false, false
	}
	if strings.HasSuffix(rest, metaSuffix) {
		return strings.TrimSuffix(rest, metaSuffix), true, true
	}
	return rest, false, true
}

// addResources registers the root, the templates, and one resource per open
// issue, and sets the completion handler.
func (m *mcpServer) addResources() {
	m.s.AddResource(&mcp.Resource{
		URI: uriScheme, Name: "issues", Title: "Open issues",
		Description: "The open issues of every served repository, one line each, newest first.",
		MIMEType:    mimeText,
	}, m.readResource)
	m.s.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: uriScheme + "{id}", Name: "issue", Title: "An issue",
		Description: "One issue as a person reads it: title, body, labels, status, linked commits, relations, the discussion in order, attachments. {id} is a full id or any unambiguous prefix.",
		MIMEType:    mimeMarkdown,
	}, m.readResource)
	m.s.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: uriScheme + "{id}" + metaSuffix, Name: "issue-meta", Title: "An issue, as data",
		Description: "The same issue as issue_show's structured content.",
		MIMEType:    mimeJSON,
	}, m.readResource)
	m.resync()
}

// readResource reads any of the URIs.
func (m *mcpServer) readResource(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := req.Params.URI
	id, meta, ok := parseURI(uri)
	if !ok {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	if id == "" {
		text, _ := m.listText()
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: mimeText, Text: text}}}, nil
	}
	r, xidr, err := m.ws.find(id)
	if err != nil {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	sh, err := ops.Show(r.Store, xidr)
	if err != nil {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	if meta {
		raw, err := json.Marshal(toShowOut(sh, r.Name))
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: mimeJSON, Text: string(raw)}}}, nil
	}
	var t textOut
	if label := repoLabel(m.ws, r); label != "" {
		fmt.Fprintf(&t, "Repository: %s\n", label)
	}
	writeShown(t.cc(), sh)
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: mimeMarkdown, Text: t.String()}}}, nil
}

// listText is the open issues across the working set, one line each, and the
// issues themselves with their repositories.
func (m *mcpServer) listText() (string, map[string]*repo) {
	var text strings.Builder
	open := map[string]*repo{}
	for _, r := range m.ws.list() {
		issues, err := ops.List(r.Store, false, "")
		if err != nil {
			continue
		}
		label := repoLabel(m.ws, r)
		for _, issue := range issues {
			open[issue.ID] = r
			if label != "" {
				text.WriteString(label + "  ")
			}
			text.WriteString(issuelib.FormatOneLiner(issue) + "\n")
		}
	}
	if len(open) == 0 {
		text.WriteString("No issues found\n")
	}
	return text.String(), open
}

// resync makes the registered resources the open issues: one added for each
// that is new, removed for each that is gone. The SDK announces the change.
func (m *mcpServer) resync() {
	m.mu.Lock()
	defer m.mu.Unlock()
	want := map[string]*mcp.Resource{}
	for _, r := range m.ws.list() {
		issues, err := ops.List(r.Store, false, "")
		if err != nil {
			continue
		}
		for _, issue := range issues {
			want[uriFor(issue.ID)] = &mcp.Resource{
				URI: uriFor(issue.ID), Name: issue.ID, Title: issue.Title,
				Description: "in " + r.Name, MIMEType: mimeMarkdown,
			}
		}
	}
	var gone []string
	for uri := range m.registered {
		if _, ok := want[uri]; !ok {
			gone = append(gone, uri)
			delete(m.registered, uri)
		}
	}
	if len(gone) > 0 {
		m.s.RemoveResources(gone...)
	}
	uris := make([]string, 0, len(want))
	for uri := range want {
		uris = append(uris, uri)
	}
	sort.Strings(uris)
	for _, uri := range uris {
		if !m.registered[uri] {
			m.s.AddResource(want[uri], m.readResource)
			m.registered[uri] = true
		}
	}
}

// changed announces that an issue changed, to whoever subscribed to either of
// its URIs.
func (m *mcpServer) changed(ctx context.Context, xidr string) {
	for _, uri := range []string{uriFor(xidr), uriFor(xidr) + metaSuffix} {
		_ = m.s.ResourceUpdated(ctx, &mcp.ResourceUpdatedNotificationParams{URI: uri})
	}
}

// complete answers the ids a prefix reaches, across the working set, for a
// host completing a template's {id}.
func (m *mcpServer) complete(_ context.Context, req *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
	res := &mcp.CompleteResult{Completion: mcp.CompletionResultDetails{Values: []string{}}}
	if req.Params.Ref == nil || req.Params.Ref.Type != "ref/resource" || req.Params.Argument.Name != "id" {
		return res, nil
	}
	prefix := strings.ToLower(req.Params.Argument.Value)
	seen := map[string]bool{}
	for _, r := range m.ws.list() {
		refs, err := r.Store.ListRefs(true)
		if err != nil {
			continue
		}
		for _, ref := range refs {
			xidr, err := issuelib.XIDRFromRef(ref)
			if err != nil || seen[xidr] || !strings.HasPrefix(xidr, prefix) {
				continue
			}
			seen[xidr] = true
		}
	}
	for xidr := range seen {
		res.Completion.Values = append(res.Completion.Values, xidr)
	}
	sort.Strings(res.Completion.Values)
	res.Completion.Total = len(res.Completion.Values)
	if len(res.Completion.Values) > completeLimit {
		res.Completion.Values = res.Completion.Values[:completeLimit]
		res.Completion.HasMore = true
	}
	return res, nil
}

// look compares every served repository's refs with the last look, announces
// each issue whose ref moved, and resyncs the listing when a ref appeared or
// went. It is what the watch does on each tick, and what a tool that pulled
// does once.
func (m *mcpServer) look(ctx context.Context) {
	m.mu.Lock()
	moved := map[string]bool{}
	listChanged := false
	for _, r := range m.ws.list() {
		tips, err := r.Store.Tips()
		if err != nil {
			continue
		}
		for ref, commit := range tips {
			key := r.Dir + " " + ref
			if was, ok := m.seen[key]; !ok || was != commit {
				if !ok {
					listChanged = true
				}
				if xidr, err := issuelib.XIDRFromRef(ref); err == nil {
					moved[xidr] = true
				}
				m.seen[key] = commit
			}
		}
		for key := range m.seen {
			dir, ref, _ := strings.Cut(key, " ")
			if dir != r.Dir {
				continue
			}
			if _, ok := tips[ref]; !ok {
				listChanged = true
				delete(m.seen, key)
			}
		}
	}
	m.mu.Unlock()
	for xidr := range moved {
		m.changed(ctx, xidr)
	}
	if listChanged {
		m.resync()
	}
}

// prime takes the first look without announcing: what is there when the server
// starts is not news.
func (m *mcpServer) prime() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.ws.list() {
		tips, err := r.Store.Tips()
		if err != nil {
			continue
		}
		for ref, commit := range tips {
			m.seen[r.Dir+" "+ref] = commit
		}
	}
}

// watch looks every interval until ctx ends. Zero is no watch.
func (m *mcpServer) watch(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.look(ctx)
			}
		}
	}()
}
