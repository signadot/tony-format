package commands

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

// notified is what a client heard: resource URIs updated, and how many times
// the listing changed.
type notified struct {
	updated     chan string
	listChanged chan struct{}
}

// mcpClientWatching connects a client that hears notifications to a server
// with a watch at the given interval.
func mcpClientWatching(t *testing.T, ws *workspace, poll time.Duration) (*mcp.ClientSession, *notified) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	serverT, clientT := mcp.NewInMemoryTransports()
	m := newMCPServer(ws)
	m.watch(ctx, poll)
	go func() {
		if err := m.s.Run(ctx, serverT); err != nil {
			t.Logf("server: %v", err)
		}
	}()
	n := &notified{updated: make(chan string, 64), listChanged: make(chan struct{}, 64)}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, &mcp.ClientOptions{
		ResourceUpdatedHandler: func(_ context.Context, req *mcp.ResourceUpdatedNotificationRequest) {
			n.updated <- req.Params.URI
		},
		ResourceListChangedHandler: func(context.Context, *mcp.ResourceListChangedRequest) {
			n.listChanged <- struct{}{}
		},
	})
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, n
}

func hearsUpdate(t *testing.T, n *notified, uri string, within time.Duration) {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case got := <-n.updated:
			if got == uri {
				return
			}
		case <-deadline:
			t.Fatalf("no update of %s within %v", uri, within)
		}
	}
}

func hearsListChanged(t *testing.T, n *notified, within time.Duration) {
	t.Helper()
	select {
	case <-n.listChanged:
	case <-time.After(within):
		t.Fatalf("no list change within %v", within)
	}
}

func drain(n *notified) {
	for {
		select {
		case <-n.updated:
		case <-n.listChanged:
		default:
			return
		}
	}
}

// TestMCP_Resources: an issue is a resource a host reads into context -- as a
// person reads it, and as data -- the open issues are listed as resources, the
// templates carry completion on the id, and an unknown id is not found
// (eg8zmb1sh12ksr48pxn0).
func TestMCP_Resources(t *testing.T) {
	dir := repoDir(t, "one")
	store := issuelib.NewGitStoreAt(dir, &strings.Builder{})
	issue, err := ops.Create(store, "A resource", "Its body.")
	if err != nil {
		t.Fatal(err)
	}
	closed, err := ops.Create(store, "Closed one", "b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ops.Close(store, closed.ID, ""); err != nil {
		t.Fatal(err)
	}
	ws := isolatedSet(t, dir)
	cs, _ := mcpClientWatching(t, ws, 0)
	ctx := context.Background()

	listed, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	uris := map[string]string{}
	for _, r := range listed.Resources {
		uris[r.URI] = r.Title
	}
	if uris[uriFor(issue.ID)] != "A resource" {
		t.Errorf("the open issue is not listed as a resource: %v", uris)
	}
	if _, ok := uris[uriFor(closed.ID)]; ok {
		t.Errorf("a closed issue is listed: %v", uris)
	}
	if _, ok := uris[uriScheme]; !ok {
		t.Errorf("the root is not listed: %v", uris)
	}
	templates, err := cs.ListResourceTemplates(ctx, nil)
	if err != nil || len(templates.ResourceTemplates) != 2 {
		t.Errorf("templates: %v, %d", err, len(templates.ResourceTemplates))
	}

	read := func(uri string) string {
		t.Helper()
		res, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
		if err != nil {
			t.Fatalf("read %s: %v", uri, err)
		}
		return res.Contents[0].Text
	}
	if text := read(uriFor(issue.ID)); !strings.Contains(text, "A resource") || !strings.Contains(text, "Its body.") {
		t.Errorf("issue://<id> reads as %q", text)
	}
	if text := read(uriFor(issue.ID[:5])); !strings.Contains(text, "A resource") {
		t.Errorf("issue://<prefix> reads as %q", text)
	}
	var meta struct{ ID, Title, Body, Status string }
	if err := json.Unmarshal([]byte(read(uriFor(issue.ID)+metaSuffix)), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.ID != issue.ID || meta.Title != "A resource" || meta.Body != "Its body." || meta.Status != "open" {
		t.Errorf("issue://<id>/meta reads as %+v", meta)
	}
	if text := read(uriScheme); !strings.Contains(text, issue.ID) || strings.Contains(text, closed.ID) {
		t.Errorf("issue:// reads as %q", text)
	}
	if _, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: uriFor("zzzz")}); err == nil {
		t.Error("an unknown id read as something")
	}

	comp, err := cs.Complete(ctx, &mcp.CompleteParams{
		Ref:      &mcp.CompleteReference{Type: "ref/resource", URI: uriScheme + "{id}"},
		Argument: mcp.CompleteParamsArgument{Name: "id", Value: issue.ID[:3]},
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range comp.Completion.Values {
		if v == issue.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("completion of %q answered %v", issue.ID[:3], comp.Completion.Values)
	}
}

// TestMCP_SubscriptionHearsTheServersOwnTools: a host subscribed to an issue
// hears an edit made through the server, and the listing changes when an issue
// is created or closed.
func TestMCP_SubscriptionHearsTheServersOwnTools(t *testing.T) {
	dir := repoDir(t, "one")
	ws := isolatedSet(t, dir)
	cs, n := mcpClientWatching(t, ws, 0)
	ctx := context.Background()

	var created struct{ ID string }
	call(t, cs, "issue_create", map[string]any{"title": "Watched", "body": "b"}, &created)
	hearsListChanged(t, n, 2*time.Second)
	drain(n)

	if err := cs.Subscribe(ctx, &mcp.SubscribeParams{URI: uriFor(created.ID)}); err != nil {
		t.Fatal(err)
	}
	call(t, cs, "issue_edit", map[string]any{"id": created.ID, "body": "edited"}, nil)
	hearsUpdate(t, n, uriFor(created.ID), 2*time.Second)
	drain(n)

	call(t, cs, "issue_close", map[string]any{"id": created.ID}, nil)
	hearsListChanged(t, n, 2*time.Second)
}

// TestMCP_WatchHearsAChangeMadeBesideTheServer: a comment written by another
// process -- here, a second store on the same repository -- is heard within
// the poll, by a host subscribed to the issue; and an issue closed beside the
// server leaves the listing.
func TestMCP_WatchHearsAChangeMadeBesideTheServer(t *testing.T) {
	dir := repoDir(t, "one")
	beside := issuelib.NewGitStoreAt(dir, &strings.Builder{})
	issue, err := ops.Create(beside, "Watched", "b")
	if err != nil {
		t.Fatal(err)
	}
	ws := isolatedSet(t, dir)
	cs, n := mcpClientWatching(t, ws, 50*time.Millisecond)
	ctx := context.Background()
	if err := cs.Subscribe(ctx, &mcp.SubscribeParams{URI: uriFor(issue.ID)}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond) // a look or two with nothing to say
	drain(n)

	if _, _, err := ops.Comment(beside, issue.ID, "from a shell"); err != nil {
		t.Fatal(err)
	}
	hearsUpdate(t, n, uriFor(issue.ID), 2*time.Second)
	drain(n)

	if _, err := ops.Close(beside, issue.ID, ""); err != nil {
		t.Fatal(err)
	}
	hearsListChanged(t, n, 2*time.Second)
	listed, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range listed.Resources {
		if r.URI == uriFor(issue.ID) {
			t.Error("the closed issue is still listed")
		}
	}
}
