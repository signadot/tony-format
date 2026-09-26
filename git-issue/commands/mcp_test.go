package commands

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/signadot/tony-format/git-issue/issuelib"
)

// mcpClient connects a client to the tracker's server over an in-memory
// transport, as a host would over stdio, and answers the session.
func mcpClient(t *testing.T, store issuelib.Store) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	server := MCPServer(store)
	go func() {
		if err := server.Run(ctx, serverT); err != nil {
			t.Logf("server: %v", err)
		}
	}()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call calls a tool and decodes its structured content into out. A tool error
// fails the test unless the caller passes wantErr.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s: %s", name, text(res))
	}
	if out != nil {
		raw, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s: structured content %s: %v", name, raw, err)
		}
	}
	return res
}

// refused calls a tool expecting a tool error, and answers its text.
func refused(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	if !res.IsError {
		t.Fatalf("%s was not refused: %s", name, text(res))
	}
	return text(res)
}

func text(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// TestMCP_ListsEveryTool: what a host sees. Push and pull are names of their own,
// which is what lets a host deny the remote and allow the rest.
func TestMCP_ListsEveryTool(t *testing.T) {
	cs := mcpClient(t, testRepo(t))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, tool := range res.Tools {
		have[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("%s has no description", tool.Name)
		}
	}
	for _, want := range []string{
		"issue_list", "issue_show", "issue_create", "issue_edit", "issue_comment", "issue_label",
		"issue_close", "issue_reopen", "issue_link", "issue_relate", "issue_for_commit",
		"issue_pull", "issue_push",
	} {
		if !have[want] {
			t.Errorf("no tool %s; have %v", want, have)
		}
	}
}

// TestMCP_AnIssuesLife runs one issue through every local tool, as an agent
// would, reading each answer as structured content -- and the text alongside is
// what the command would have printed.
func TestMCP_AnIssuesLife(t *testing.T) {
	store := testRepo(t)
	cs := mcpClient(t, store)

	var created struct{ ID, Ref string }
	res := call(t, cs, "issue_create", map[string]any{"title": "A thing", "body": "The body.", "labels": []string{"bug"}}, &created)
	if len(created.ID) != 20 || !strings.Contains(text(res), "Created issue "+created.ID) {
		t.Fatalf("create answered %+v, %q", created, text(res))
	}
	if msg := refused(t, cs, "issue_create", map[string]any{"title": "Empty", "body": " "}); !strings.Contains(msg, "description cannot be empty") {
		t.Errorf("an empty body was refused with %q", msg)
	}

	var shown struct {
		ID, Status, Title, Body string
		Labels                  []string
		Comments                []struct{ Path, Text string }
	}
	call(t, cs, "issue_show", map[string]any{"id": created.ID[:4]}, &shown)
	if shown.ID != created.ID || shown.Status != "open" || shown.Title != "A thing" || shown.Body != "The body." || strings.Join(shown.Labels, ",") != "bug" {
		t.Errorf("shown as %+v", shown)
	}

	call(t, cs, "issue_edit", map[string]any{"id": created.ID, "body": "## Decided\n\nThe new body."}, nil)
	call(t, cs, "issue_comment", map[string]any{"id": created.ID, "text": "a finding"}, nil)
	var labels struct{ Labels []string }
	call(t, cs, "issue_label", map[string]any{"id": created.ID, "add": []string{"git-issue-phase=planned"}, "remove": []string{"bug"}}, &labels)
	if strings.Join(labels.Labels, ",") != "git-issue-phase=planned" {
		t.Errorf("labels %v", labels.Labels)
	}
	res = call(t, cs, "issue_show", map[string]any{"id": created.ID}, &shown)
	if shown.Body != "## Decided\n\nThe new body." || len(shown.Comments) != 1 || shown.Comments[0].Text != "a finding" {
		t.Errorf("after edit and comment: %+v", shown)
	}
	if !strings.Contains(text(res), "Discussion:") || !strings.Contains(text(res), "## Decided") {
		t.Errorf("show's text is not what the command prints: %q", text(res))
	}

	var other struct{ ID string }
	call(t, cs, "issue_create", map[string]any{"title": "Another", "body": "b"}, &other)
	var rel struct{ Changed bool }
	call(t, cs, "issue_relate", map[string]any{"id": created.ID, "other": other.ID, "kind": "blocks"}, &rel)
	if !rel.Changed {
		t.Error("the first relate changed nothing")
	}
	call(t, cs, "issue_relate", map[string]any{"id": created.ID, "other": other.ID, "kind": "blocks"}, &rel)
	if rel.Changed {
		t.Error("the second relate changed something")
	}
	if msg := refused(t, cs, "issue_relate", map[string]any{"id": created.ID, "other": other.ID, "kind": "owns"}); !strings.Contains(msg, "unknown relation") {
		t.Errorf("an unknown relation was refused with %q", msg)
	}

	head := strings.TrimSpace(run(t, "", "rev-parse", "HEAD"))
	var linked struct{ Commit string }
	call(t, cs, "issue_link", map[string]any{"id": created.ID, "commit": "HEAD"}, &linked)
	if linked.Commit != head {
		t.Errorf("linked %q, want %q", linked.Commit, head)
	}
	var forCommit struct{ Issues []struct{ ID, Title string } }
	call(t, cs, "issue_for_commit", map[string]any{"commit": head}, &forCommit)
	if len(forCommit.Issues) != 1 || forCommit.Issues[0].ID != created.ID {
		t.Errorf("for-commit: %+v", forCommit)
	}

	var status struct {
		Status   string
		ClosedBy string `json:"closed_by"`
	}
	call(t, cs, "issue_close", map[string]any{"id": created.ID, "commit": head}, &status)
	if status.Status != "closed" || status.ClosedBy != head {
		t.Errorf("close answered %+v", status)
	}
	if msg := refused(t, cs, "issue_close", map[string]any{"id": created.ID}); !strings.Contains(msg, "already closed") {
		t.Errorf("closing a closed issue was refused with %q", msg)
	}
	var list struct{ Issues []struct{ ID, Status string } }
	call(t, cs, "issue_list", map[string]any{}, &list)
	if len(list.Issues) != 1 || list.Issues[0].ID != other.ID {
		t.Errorf("open list: %+v", list)
	}
	call(t, cs, "issue_list", map[string]any{"all": true, "label": "git-issue-phase=planned"}, &list)
	if len(list.Issues) != 1 || list.Issues[0].ID != created.ID || list.Issues[0].Status != "closed" {
		t.Errorf("all by label: %+v", list)
	}
	call(t, cs, "issue_reopen", map[string]any{"id": created.ID}, &status)
	if status.Status != "open" {
		t.Errorf("reopen answered %+v", status)
	}
	if msg := refused(t, cs, "issue_show", map[string]any{"id": "zzzzzz"}); !strings.Contains(msg, "not found") {
		t.Errorf("an unknown id was refused with %q", msg)
	}
}

// TestMCP_PushAndPull: the two tools that touch the remote, through the same two
// clones the command tests use. A dry run writes nothing and says what it would.
func TestMCP_PushAndPull(t *testing.T) {
	storeA, origin := pushTestRepo(t)
	csA := mcpClient(t, storeA)
	var created struct{ ID string }
	call(t, csA, "issue_create", map[string]any{"title": "Shared", "body": "b"}, &created)

	var report struct {
		DryRun    bool `json:"dry_run"`
		Changed   []struct{ ID, What string }
		Unchanged int
		Whole     bool
	}
	call(t, csA, "issue_push", map[string]any{"dry_run": true}, &report)
	if !report.DryRun || len(report.Changed) != 1 || report.Changed[0].ID != created.ID || !strings.Contains(report.Changed[0].What, "send to the remote") {
		t.Errorf("a dry push answered %+v", report)
	}
	if refs := remoteIssueRefs(t, origin); len(refs) != 0 {
		t.Errorf("a dry push wrote %v", refs)
	}
	call(t, csA, "issue_push", map[string]any{}, &report)
	if len(report.Changed) != 1 || !report.Whole {
		t.Errorf("the push answered %+v", report)
	}
	if refs := remoteIssueRefs(t, origin); len(refs) != 1 {
		t.Errorf("after the push the remote holds %v", refs)
	}

	b := secondClone(t, origin)
	inClone(t, b, func(storeB issuelib.Store) {
		csB := mcpClient(t, storeB)
		call(t, csB, "issue_pull", map[string]any{}, &report)
		if len(report.Changed) != 1 || report.Changed[0].ID != created.ID || !report.Whole {
			t.Errorf("the pull answered %+v", report)
		}
		var shown struct{ Title string }
		call(t, csB, "issue_show", map[string]any{"id": created.ID}, &shown)
		if shown.Title != "Shared" {
			t.Errorf("B reads the pulled issue as %+v", shown)
		}
	})
}
