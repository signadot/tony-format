package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

// The tools, one per operation, thin over ops. Each In struct is the tool's
// input schema -- the SDK infers it, and reads the descriptions from the
// jsonschema tags -- and each Out is its structured content. What a tool says
// in its description is what the agent reads instead of a CLAUDE.md, so the
// tracker's rules sit next to the operation they govern.
//
// A tool given an id asks the working set which repository holds it
// (workspace.find) and calls ops on that repository's store with the id in
// full. A tool that makes or lists or syncs needs a repository named when more
// than one is served (workspace.byName).

// linkedOut is an issue named by another, or by a commit's note.
type linkedOut struct {
	ID     string `json:"id"`
	Status string `json:"status,omitempty"`
	Title  string `json:"title,omitempty"`
	Error  string `json:"error,omitempty" jsonschema:"why the issue could not be read, when it could not"`
}

func toLinked(in []ops.Linked) []linkedOut {
	out := make([]linkedOut, 0, len(in))
	for _, l := range in {
		out = append(out, linkedOut{ID: l.ID, Status: l.Status, Title: l.Title, Error: l.Err})
	}
	return out
}

type issueRow struct {
	ID      string    `json:"id"`
	Repo    string    `json:"repo,omitempty" jsonschema:"the repository, when more than one is served"`
	Status  string    `json:"status"`
	Title   string    `json:"title"`
	Labels  []string  `json:"labels"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
}

func toRow(i *issuelib.Issue, repo string) issueRow {
	labels := i.Labels
	if labels == nil {
		labels = []string{}
	}
	return issueRow{
		ID: i.ID, Repo: repo, Status: issuelib.StatusOf(i), Title: i.Title,
		Labels: labels, Created: i.Created, Updated: i.Updated,
	}
}

type listIn struct {
	Repo  string `json:"repo,omitempty" jsonschema:"one repository; every served repository when empty"`
	All   bool   `json:"all,omitempty" jsonschema:"include closed issues and mirrors; open only by default"`
	Label string `json:"label,omitempty" jsonschema:"only issues carrying this label (a plain label, or key=value)"`
}

type listOut struct {
	Issues []issueRow `json:"issues" jsonschema:"newest first"`
}

type showIn struct {
	ID string `json:"id" jsonschema:"the issue: a full id or any unambiguous prefix"`
}

type commentOut struct {
	Path string    `json:"path" jsonschema:"the comment's file under discussion/"`
	When time.Time `json:"when,omitempty"`
	Text string    `json:"text"`
}

type showOut struct {
	ID          string       `json:"id"`
	Repo        string       `json:"repo"`
	Status      string       `json:"status"`
	Ref         string       `json:"ref"`
	Source      string       `json:"source,omitempty" jsonschema:"for a mirror, the repository it is mirrored from; it is read-only here"`
	Title       string       `json:"title"`
	Body        string       `json:"body" jsonschema:"the description without its title line"`
	Labels      []string     `json:"labels"`
	Created     time.Time    `json:"created"`
	Updated     time.Time    `json:"updated"`
	ClosedBy    string       `json:"closed_by,omitempty" jsonschema:"the commit that closed it, when one was given"`
	Commits     []string     `json:"commits" jsonschema:"linked commits, one line each as git shows them"`
	Branches    []string     `json:"branches"`
	Related     []linkedOut  `json:"related"`
	Blocks      []linkedOut  `json:"blocks"`
	BlockedBy   []linkedOut  `json:"blocked_by"`
	Duplicates  []linkedOut  `json:"duplicates"`
	Comments    []commentOut `json:"comments" jsonschema:"in chronological order"`
	Attachments []string     `json:"attachments" jsonschema:"paths under discussion/files/"`
}

func toShowOut(sh *ops.Shown, repo string) showOut {
	out := showOut{
		ID: sh.Issue.ID, Repo: repo, Status: sh.Status, Ref: sh.Ref, Source: sh.Source,
		Title: sh.Title, Body: sh.Body,
		Labels: sh.Issue.Labels, Created: sh.Issue.Created, Updated: sh.Issue.Updated,
		Commits: sh.Commits, Branches: sh.Issue.Branches,
		Related: toLinked(sh.Related), Blocks: toLinked(sh.Blocks),
		BlockedBy: toLinked(sh.BlockedBy), Duplicates: toLinked(sh.Duplicates),
		Attachments: sh.Attachments,
	}
	if sh.Issue.ClosedBy != nil {
		out.ClosedBy = *sh.Issue.ClosedBy
	}
	for _, c := range sh.Comments {
		out.Comments = append(out.Comments, commentOut{Path: c.Path, When: c.When, Text: strings.TrimRight(c.Text, "\n")})
	}
	for _, p := range []*[]string{&out.Labels, &out.Commits, &out.Branches, &out.Attachments} {
		if *p == nil {
			*p = []string{}
		}
	}
	if out.Comments == nil {
		out.Comments = []commentOut{}
	}
	return out
}

type createIn struct {
	Repo   string   `json:"repo,omitempty" jsonschema:"the repository to file it in; required when more than one is served"`
	Title  string   `json:"title"`
	Body   string   `json:"body" jsonschema:"the description, markdown; required"`
	Labels []string `json:"labels,omitempty"`
}

type idOut struct {
	ID   string `json:"id"`
	Repo string `json:"repo,omitempty"`
	Ref  string `json:"ref,omitempty"`
}

type editIn struct {
	ID    string `json:"id" jsonschema:"the issue: a full id or any unambiguous prefix"`
	Title string `json:"title,omitempty" jsonschema:"the new title; the current one is kept when this is empty"`
	Body  string `json:"body,omitempty" jsonschema:"the new body, markdown; the current one is kept when this is empty"`
}

type commentIn struct {
	ID   string `json:"id" jsonschema:"the issue: a full id or any unambiguous prefix"`
	Text string `json:"text" jsonschema:"the comment, markdown"`
}

type commentedOut struct {
	ID   string `json:"id"`
	Path string `json:"path" jsonschema:"where the comment was stored, under discussion/"`
}

type labelIn struct {
	ID     string   `json:"id" jsonschema:"the issue: a full id or any unambiguous prefix"`
	Add    []string `json:"add,omitempty" jsonschema:"labels to add; key=value replaces the key's value"`
	Remove []string `json:"remove,omitempty" jsonschema:"labels to remove; a bare key removes it whatever its value"`
}

type labelsOut struct {
	ID     string   `json:"id"`
	Labels []string `json:"labels" jsonschema:"the issue's labels afterwards"`
}

type closeIn struct {
	ID     string `json:"id" jsonschema:"the issue: a full id or any unambiguous prefix"`
	Commit string `json:"commit,omitempty" jsonschema:"the commit that closes it: a SHA, or anything git resolves in the issue's repository"`
}

type statusOut struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	ClosedBy string `json:"closed_by,omitempty"`
}

type linkIn struct {
	ID     string `json:"id" jsonschema:"the issue: a full id or any unambiguous prefix"`
	Commit string `json:"commit" jsonschema:"a SHA, or anything git resolves in the issue's repository"`
}

type linkOut struct {
	ID     string `json:"id"`
	Commit string `json:"commit" jsonschema:"the full SHA"`
}

type relateIn struct {
	ID    string `json:"id" jsonschema:"the issue the relation is recorded on"`
	Other string `json:"other" jsonschema:"the issue it relates to; in another served repository, it is mirrored into this one first"`
	Kind  string `json:"kind" jsonschema:"related, blocks (id blocks other) or duplicate (id duplicates other)"`
}

type relateOut struct {
	ID       string `json:"id"`
	Other    string `json:"other"`
	Kind     string `json:"kind"`
	Changed  bool   `json:"changed" jsonschema:"false when the relation was already recorded"`
	Mirrored string `json:"mirrored,omitempty" jsonschema:"the repository the other issue was mirrored from, when it was"`
}

type forCommitIn struct {
	Repo   string `json:"repo,omitempty" jsonschema:"the repository the commit is in; required when more than one is served"`
	Commit string `json:"commit" jsonschema:"a SHA, or anything git resolves"`
}

type forCommitOut struct {
	Commit string      `json:"commit" jsonschema:"the full SHA"`
	Issues []linkedOut `json:"issues"`
}

type pullIn struct {
	Repo   string `json:"repo,omitempty" jsonschema:"the repository; required when more than one is served"`
	Remote string `json:"remote,omitempty" jsonschema:"the git remote; origin by default"`
	Force  bool   `json:"force,omitempty" jsonschema:"where an issue was edited on both sides, take the remote side"`
	DryRun bool   `json:"dry_run,omitempty" jsonschema:"say what a pull would do, and write nothing"`
}

type pushIn struct {
	Repo   string `json:"repo,omitempty" jsonschema:"the repository; required when more than one is served"`
	Remote string `json:"remote,omitempty" jsonschema:"the git remote; origin by default"`
	ID     string `json:"id,omitempty" jsonschema:"one issue to push; every issue when empty"`
	Force  bool   `json:"force,omitempty" jsonschema:"where an issue was edited on both sides, take this clone"`
	DryRun bool   `json:"dry_run,omitempty" jsonschema:"say what a push would do, and write nothing"`
}

type changeOut struct {
	ID   string `json:"id"`
	What string `json:"what"`
}

type refusalOut struct {
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	Reason string `json:"reason"`
	Here   string `json:"here" jsonschema:"this clone's commit, abbreviated, or nothing"`
	There  string `json:"there" jsonschema:"the remote's commit, abbreviated, or nothing"`
	Split  bool   `json:"split,omitempty" jsonschema:"the remote holds the issue at two tips that disagree"`
}

type reportOut struct {
	Repo      string            `json:"repo"`
	Remote    string            `json:"remote"`
	DryRun    bool              `json:"dry_run"`
	Changed   []changeOut       `json:"changed" jsonschema:"issues something happened to, or would"`
	Unchanged int               `json:"unchanged"`
	Refused   []refusalOut      `json:"refused" jsonschema:"issues left alone for a person to decide"`
	OldClient []string          `json:"old_client" jsonschema:"issues pushed by a git-issue older than this one"`
	Failed    []string          `json:"failed"`
	Cleaned   int               `json:"cleaned,omitempty" jsonschema:"stale refs a pull removed"`
	Refreshed map[string]int    `json:"refreshed,omitempty" jsonschema:"mirrors a pull refreshed, by source"`
	Unreached map[string]string `json:"unreached,omitempty" jsonschema:"sources a pull could not reach, and why; their mirrors are as they were"`
	Whole     bool              `json:"whole" jsonschema:"true when nothing was refused or failed"`
}

func toReportOut(r *ops.Report, repo string) reportOut {
	out := reportOut{
		Repo: repo, Remote: r.Remote, DryRun: r.DryRun, Unchanged: r.Unchanged, Cleaned: r.Cleaned,
		Changed: []changeOut{}, Refused: []refusalOut{}, OldClient: []string{}, Failed: []string{},
		Refreshed: r.Refreshed, Unreached: r.Unreached,
		Whole: r.Err() == nil,
	}
	for _, c := range r.Changed {
		out.Changed = append(out.Changed, changeOut{ID: c.ID, What: c.What})
	}
	for _, f := range r.Refused {
		out.Refused = append(out.Refused, refusalOut{ID: f.ID, Title: f.Title, Reason: f.Reason, Here: f.Here, There: f.There, Split: f.Split})
	}
	out.OldClient = append(out.OldClient, r.OldClient...)
	for _, err := range r.Failed {
		out.Failed = append(out.Failed, err.Error())
	}
	return out
}

type repoAddIn struct {
	Path    string `json:"path" jsonschema:"the repository's directory"`
	Persist bool   `json:"persist,omitempty" jsonschema:"also record it in ~/.config/git-issue.tony, so the next server serves it too"`
}

type repoIn struct {
	Repo string `json:"repo" jsonschema:"the repository's name, as repo_list gives it"`
}

type repoRow struct {
	Name string `json:"name"`
	Path string `json:"path"`
	From string `json:"from" jsonschema:"where the server got it: -C, config, cwd, repo_add"`
}

type reposOut struct {
	Repos []repoRow `json:"repos"`
}

func toRepos(rs []*repo) reposOut {
	out := reposOut{Repos: []repoRow{}}
	for _, r := range rs {
		out.Repos = append(out.Repos, repoRow{Name: r.Name, Path: r.Dir, From: r.From})
	}
	return out
}

func readOnly() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(bool)}
}

// local is a tool that writes to the repository and nothing beyond it.
func local(idempotent bool) *mcp.ToolAnnotations {
	f := false
	return &mcp.ToolAnnotations{DestructiveHint: &f, IdempotentHint: idempotent, OpenWorldHint: new(bool)}
}

// repoLabel is the repository a row names: nothing when one is served, since
// there is nothing to tell apart.
func repoLabel(ws *workspace, r *repo) string {
	if len(ws.list()) > 1 {
		return r.Name
	}
	return ""
}

// addTools registers every tool on s, over the working set.
func addTools(s *mcp.Server, ws *workspace) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "issue_list",
		Description: "List issues, newest first: open ones, or all of them (closed and mirrors too), and only those carrying a label when one is given; from one repository, or every served one.",
		Annotations: readOnly(),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, listOut, error) {
		repos := ws.list()
		if in.Repo != "" {
			r, err := ws.byName(in.Repo)
			if err != nil {
				return nil, listOut{}, err
			}
			repos = []*repo{r}
		}
		out := listOut{Issues: []issueRow{}}
		var text strings.Builder
		for _, r := range repos {
			issues, err := ops.List(r.Store, in.All, in.Label)
			if err != nil {
				return nil, listOut{}, err
			}
			label := repoLabel(ws, r)
			for _, issue := range issues {
				out.Issues = append(out.Issues, toRow(issue, label))
				if label != "" {
					text.WriteString(label + "  ")
				}
				text.WriteString(issuelib.FormatOneLiner(issue) + "\n")
			}
		}
		if len(out.Issues) == 0 {
			text.WriteString("No issues found\n")
		}
		return result(text.String()), out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "issue_show",
		Description: "Read one issue whole: its title, body, labels, status, linked commits, relations to other issues, the discussion in order, and its attachments by name.",
		Annotations: readOnly(),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in showIn) (*mcp.CallToolResult, showOut, error) {
		r, xidr, err := ws.find(in.ID)
		if err != nil {
			return nil, showOut{}, err
		}
		sh, err := ops.Show(r.Store, xidr)
		if err != nil {
			return nil, showOut{}, err
		}
		var t textOut
		if label := repoLabel(ws, r); label != "" {
			fmt.Fprintf(&t, "Repository: %s\n", label)
		}
		writeShown(t.cc(), sh)
		return result(t.String()), toShowOut(sh, r.Name), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "issue_create",
		Description: "File an issue with a title and a body (markdown), and labels if any, in a repository. Every change to the code gets an issue, " +
			"filed before the work, and the commit that makes the change carries \"Issue: <full id>\" as a trailer -- this tool answers the full id.",
		Annotations: local(false),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in createIn) (*mcp.CallToolResult, idOut, error) {
		r, err := ws.byName(in.Repo)
		if err != nil {
			return nil, idOut{}, err
		}
		issue, err := ops.Create(r.Store, in.Title, in.Body)
		if err != nil {
			return nil, idOut{}, err
		}
		if len(in.Labels) > 0 {
			if _, err := ops.Label(r.Store, issue.ID, in.Labels, nil); err != nil {
				return nil, idOut{}, fmt.Errorf("created %s, but its labels were refused: %w", issue.ID, err)
			}
		}
		return result(fmt.Sprintf("Created issue %s in %s\nRef: %s\n", issue.ID, r.Name, issue.Ref)),
			idOut{ID: issue.ID, Repo: r.Name, Ref: issue.Ref}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "issue_edit",
		Description: "Change what an issue says: its title, its body, or both; what is not given is kept. The edit is a commit on the " +
			"issue's chain, so history keeps what it said before. To record a decision without rewriting the issue, comment instead.",
		Annotations: local(true),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in editIn) (*mcp.CallToolResult, idOut, error) {
		r, xidr, err := ws.find(in.ID)
		if err != nil {
			return nil, idOut{}, err
		}
		var title, body *string
		if in.Title != "" {
			title = &in.Title
		}
		if in.Body != "" {
			body = &in.Body
		}
		issue, err := ops.Edit(r.Store, xidr, title, body)
		if err != nil {
			return nil, idOut{}, err
		}
		return result("Edited issue " + issue.ID + "\n"), idOut{ID: issue.ID, Repo: r.Name, Ref: issue.Ref}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "issue_comment",
		Description: "Add a comment (markdown) to an issue's discussion. This is where a decision, a finding or a question is recorded.",
		Annotations: local(false),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in commentIn) (*mcp.CallToolResult, commentedOut, error) {
		r, xidr, err := ws.find(in.ID)
		if err != nil {
			return nil, commentedOut{}, err
		}
		issue, path, err := ops.Comment(r.Store, xidr, in.Text)
		if err != nil {
			return nil, commentedOut{}, err
		}
		return result(fmt.Sprintf("Added comment to issue %s (%s)\n", issue.ID, strings.TrimPrefix(path, "discussion/"))),
			commentedOut{ID: issue.ID, Path: path}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "issue_label",
		Description: "Add and remove labels. A label is lowercased. One containing \"=\" is a key and a value, and a key holds one value: " +
			"adding key=value replaces the key's value, removing a bare key removes it whatever its value. Labels beginning git-issue- are reserved.",
		Annotations: local(true),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in labelIn) (*mcp.CallToolResult, labelsOut, error) {
		r, xidr, err := ws.find(in.ID)
		if err != nil {
			return nil, labelsOut{}, err
		}
		issue, err := ops.Label(r.Store, xidr, in.Add, in.Remove)
		if err != nil {
			return nil, labelsOut{}, err
		}
		labels := issue.Labels
		if labels == nil {
			labels = []string{}
		}
		return result(fmt.Sprintf("Labels: %s\n", strings.Join(labels, ", "))), labelsOut{ID: issue.ID, Labels: labels}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "issue_close",
		Description: "Close an open issue. A fix closes its issue with the commit that made it: pass that commit. " +
			"An issue already closed is refused.",
		Annotations: local(false),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in closeIn) (*mcp.CallToolResult, statusOut, error) {
		r, xidr, err := ws.find(in.ID)
		if err != nil {
			return nil, statusOut{}, err
		}
		issue, err := ops.Close(r.Store, xidr, in.Commit)
		if err != nil {
			return nil, statusOut{}, err
		}
		out := statusOut{ID: issue.ID, Status: "closed"}
		text := "Closed issue " + issue.ID + "\n"
		if issue.ClosedBy != nil {
			out.ClosedBy = *issue.ClosedBy
			text += "Closed by: " + (*issue.ClosedBy)[:7] + "\n"
		}
		return result(text), out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "issue_reopen",
		Description: "Reopen a closed issue. An open issue is refused.",
		Annotations: local(false),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in showIn) (*mcp.CallToolResult, statusOut, error) {
		r, xidr, err := ws.find(in.ID)
		if err != nil {
			return nil, statusOut{}, err
		}
		issue, err := ops.Reopen(r.Store, xidr)
		if err != nil {
			return nil, statusOut{}, err
		}
		return result("Reopened issue " + issue.ID + "\n"), statusOut{ID: issue.ID, Status: "open"}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "issue_link",
		Description: "Link an issue to a commit of its repository, in both directions: the commit is recorded on the issue, and the issue on " +
			"the commit as a git note, so issue_for_commit finds it.",
		Annotations: local(true),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in linkIn) (*mcp.CallToolResult, linkOut, error) {
		r, xidr, err := ws.find(in.ID)
		if err != nil {
			return nil, linkOut{}, err
		}
		issue, sha, err := ops.Link(r.Store, xidr, in.Commit)
		if err != nil {
			return nil, linkOut{}, err
		}
		return result(fmt.Sprintf("Linked issue %s to commit %s\n", issue.ID, sha[:7])), linkOut{ID: issue.ID, Commit: sha}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "issue_relate",
		Description: "Record how one issue stands to another: related; blocks (the first blocks the second, recorded on both); " +
			"or duplicate (the first duplicates the second). When the other issue is in another served repository, it is mirrored into " +
			"the first's repository as a read-only ext reference first, so the relation resolves from that repository alone.",
		Annotations: local(true),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in relateIn) (*mcp.CallToolResult, relateOut, error) {
		from, fromID, err := ws.find(in.ID)
		if err != nil {
			return nil, relateOut{}, err
		}
		to, toID, err := ws.find(in.Other)
		if err != nil {
			return nil, relateOut{}, err
		}
		out := relateOut{ID: fromID, Other: toID, Kind: in.Kind}
		if to != from {
			// The far issue comes here first. The source is named for the other
			// repository, and is its directory: the fetch is local.
			if err := ops.SourceAdd(from.Store, to.Name, to.Dir); err != nil {
				return nil, relateOut{}, err
			}
			if _, err := ops.Mirror(from.Store, to.Name, toID); err != nil {
				return nil, relateOut{}, err
			}
			out.Mirrored = to.Name
		}
		_, _, changed, err := ops.Relate(from.Store, fromID, toID, ops.Relation(in.Kind))
		if err != nil {
			return nil, relateOut{}, err
		}
		out.Changed = changed
		text := fmt.Sprintf("Issue %s: %s %s\n", fromID, in.Kind, toID)
		if !changed {
			text = fmt.Sprintf("Issue %s already has this relationship with %s\n", fromID, toID)
		}
		if out.Mirrored != "" {
			text += fmt.Sprintf("%s is mirrored from %s into %s, read-only there\n", toID, to.Name, from.Name)
		}
		return result(text), out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "issue_for_commit",
		Description: "The issues linked to a commit of a repository, through its note.",
		Annotations: readOnly(),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in forCommitIn) (*mcp.CallToolResult, forCommitOut, error) {
		r, err := ws.byName(in.Repo)
		if err != nil {
			return nil, forCommitOut{}, err
		}
		sha, linked, err := ops.ForCommit(r.Store, in.Commit)
		if err != nil {
			return nil, forCommitOut{}, err
		}
		var t textOut
		cc := t.cc()
		if len(linked) == 0 {
			fmt.Fprintf(cc.Out, "No issues linked to commit %s\n", sha[:7])
		}
		for _, l := range linked {
			writeLinked(cc, l)
		}
		return result(t.String()), forCommitOut{Commit: sha, Issues: toLinked(linked)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "issue_pull",
		Description: "Bring the remote's issues into a repository, and its mirrors up to their sources. An issue edited on both sides is " +
			"merged when the two can be brought together, and otherwise refused and named for a person to decide; force takes the " +
			"remote side outright. dry_run says what a pull would do and writes nothing.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(_ context.Context, _ *mcp.CallToolRequest, in pullIn) (*mcp.CallToolResult, reportOut, error) {
		r, err := ws.byName(in.Repo)
		if err != nil {
			return nil, reportOut{}, err
		}
		report, err := ops.Pull(r.Store, in.Remote, in.Force, in.DryRun)
		if err != nil {
			return nil, reportOut{}, err
		}
		var t textOut
		_ = writeReport(t.cc(), report)
		return result(t.String()), toReportOut(report, r.Name), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "issue_push",
		Description: "Send a repository's issues to its remote, mirrors and sources with them: one issue, or every issue when id is empty. " +
			"Every write carries a lease on what the last fetch saw, so an issue edited on both sides is refused and named rather than " +
			"overwritten; force takes this clone's side. dry_run says what a push would do and writes nothing.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(_ context.Context, _ *mcp.CallToolRequest, in pushIn) (*mcp.CallToolResult, reportOut, error) {
		r, err := ws.byName(in.Repo)
		if err != nil {
			return nil, reportOut{}, err
		}
		id := in.ID
		if id != "" {
			found, xidr, err := ws.find(id)
			if err != nil {
				return nil, reportOut{}, err
			}
			if found != r {
				return nil, reportOut{}, fmt.Errorf("%s is in %s, not %s", xidr, found.Name, r.Name)
			}
			id = xidr
		}
		report, err := ops.Push(r.Store, in.Remote, id, in.Force, in.DryRun)
		if err != nil {
			return nil, reportOut{}, err
		}
		var t textOut
		_ = writeReport(t.cc(), report)
		return result(t.String()), toReportOut(report, r.Name), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "repo_list",
		Description: "The repositories this server serves -- the working set -- and where each came from. " +
			"An id given to any tool is looked for in every one of them.",
		Annotations: readOnly(),
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, reposOut, error) {
		out := toRepos(ws.list())
		var text strings.Builder
		for _, r := range out.Repos {
			fmt.Fprintf(&text, "%s  %s  (%s)\n", r.Name, r.Path, r.From)
		}
		if len(out.Repos) == 0 {
			text.WriteString("No repositories served\n")
		}
		return result(text.String()), out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "repo_add",
		Description: "Serve a repository from now on, by its directory. With persist, it is also recorded in ~/.config/git-issue.tony " +
			"so the next server serves it too. The set is the user's configuration; the issues stay in their repositories.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, OpenWorldHint: new(bool)},
	}, func(_ context.Context, _ *mcp.CallToolRequest, in repoAddIn) (*mcp.CallToolResult, reposOut, error) {
		r, err := ws.add(in.Path, "repo_add")
		if err != nil {
			return nil, reposOut{}, err
		}
		if in.Persist {
			if err := persistWorkingSet(r.Dir); err != nil {
				return nil, reposOut{}, fmt.Errorf("serving %s, but could not record it: %w", r.Name, err)
			}
		}
		return result(fmt.Sprintf("Serving %s (%s)\n", r.Name, r.Dir)), toRepos(ws.list()), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "repo_remove",
		Description: "Stop serving a repository. Its issues are where they were.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, OpenWorldHint: new(bool)},
	}, func(_ context.Context, _ *mcp.CallToolRequest, in repoIn) (*mcp.CallToolResult, reposOut, error) {
		if err := ws.remove(in.Repo); err != nil {
			return nil, reposOut{}, err
		}
		return result(fmt.Sprintf("No longer serving %s\n", in.Repo)), toRepos(ws.list()), nil
	})
}
