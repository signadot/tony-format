package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/signadot/tony-format/git-issue/issuelib"
)

// issue_watch is the server's watch in the agent's hands. A resource
// subscription reaches an agent only if its host subscribes and passes the
// notification on, and a host need not: Claude Code gives its agent no
// subscribe (vegmw7bmh12ks11mq1n0). So what each look finds (look, in
// mcp_resources.go) is also numbered and kept, and issue_watch answers the
// changes after a cursor -- at once when there are some, or when the next
// arrives, or with none at its timeout.

const (
	defaultWatchTimeout = 5 * time.Minute
	maxWatchTimeout     = time.Hour
)

type watchIn struct {
	Repo    []string `json:"repo,omitempty" jsonschema:"the repositories to watch, by name; every served one when empty"`
	IDs     []string `json:"ids,omitempty" jsonschema:"the issues to watch: full ids or unambiguous prefixes; every issue when empty"`
	Label   string   `json:"label,omitempty" jsonschema:"only issues carrying this label before the change or after it, so its removal is heard (a plain label, or key=value)"`
	Since   string   `json:"since,omitempty" jsonschema:"the cursor a previous issue_watch answered: changes after it are answered at once, so none is missed between calls. Without it, only changes from now on"`
	Timeout int      `json:"timeout,omitempty" jsonschema:"seconds to wait for a change before answering with none; 300 by default, at most 3600"`
}

type watchRemoteIn struct {
	Remote  string   `json:"remote,omitempty" jsonschema:"the remote to pull; origin by default"`
	Repo    []string `json:"repo,omitempty" jsonschema:"the repositories to watch and pull, by name; every served one when empty"`
	IDs     []string `json:"ids,omitempty" jsonschema:"the issues to watch: full ids or unambiguous prefixes; every issue when empty"`
	Label   string   `json:"label,omitempty" jsonschema:"only issues carrying this label before the change or after it, so its removal is heard (a plain label, or key=value)"`
	Since   string   `json:"since,omitempty" jsonschema:"the cursor a previous issue_watch or issue_watch_remote answered: changes after it are answered at once, so none is missed between calls. Without it, only changes from now on"`
	Timeout int      `json:"timeout,omitempty" jsonschema:"seconds to wait for a change before answering with none; 300 by default, at most 3600"`
}

type watchOut struct {
	Changes []watchChange `json:"changes" jsonschema:"the issues that changed, in the order the changes were found; empty at the timeout"`
	Pulls   []pullNote    `json:"pulls,omitempty" jsonschema:"issue_watch_remote: what its pulls did to the issues among changes, and -- when it differs from what the cursor was last told -- all that stands: refusals, failures, or clear"`
	Cursor  string        `json:"cursor" jsonschema:"pass as since to the next issue_watch"`
}

// watchPulls is what issue_watch_remote pulls while it waits: a remote, in
// each of some repositories.
type watchPulls struct {
	remote string
	repos  []*repo
}

func addWatchTool(m *mcpServer) {
	mcp.AddTool(m.s, &mcp.Tool{
		Name: "issue_watch",
		Description: "Wait for issues to change -- a comment, an edit, a label, a close or reopen, an issue filed or pulled -- by this " +
			"server's tools or by anything else that moves their refs in this clone (a shell, a pull, another agent), and answer what " +
			"changed. It reads this clone only: a change pushed to a remote is heard once a pull brings it in, which issue_watch_remote does. " +
			"Scope it by repo, ids and label, which all must match; with none, every issue in every served repository. It answers " +
			"as soon as a change matches, or with none at the timeout, and always with a cursor: pass it back as since and nothing " +
			"that changed between two calls is missed. It blocks while it waits, so call it where waiting does not hold up other work.",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in watchIn) (*mcp.CallToolResult, watchOut, error) {
		return m.watchTool(ctx, in.Repo, in.IDs, in.Label, in.Since, in.Timeout, nil)
	})

	mcp.AddTool(m.s, &mcp.Tool{
		Name: "issue_watch_remote",
		Description: "issue_watch that also pulls: when it starts and every -fetch (the server's, 30 seconds by default) it pulls a remote, origin by " +
			"default, into each repository watched, so a change a teammate pushed is heard as well as one made here. It answers " +
			"what changed, and what its pulls did: issues created, moved on or merged, one refused for a person to decide, a remote that could " +
			"not be reached. What stands until a person acts -- a refusal, a failure -- answers on its own when it differs from what the cursor was last told, or clear when nothing stands. It writes this clone's refs as issue_pull " +
			"does, and nothing to the remote. Its cursor is issue_watch's.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in watchRemoteIn) (*mcp.CallToolResult, watchOut, error) {
		remote := in.Remote
		if remote == "" {
			remote = "origin"
		}
		return m.watchTool(ctx, in.Repo, in.IDs, in.Label, in.Since, in.Timeout, &watchPulls{remote: remote})
	})
}

// watchTool is either watch tool: the scopes resolved, the wait, the answer.
func (m *mcpServer) watchTool(ctx context.Context, repos, ids []string, label, since string, secs int, pulls *watchPulls) (*mcp.CallToolResult, watchOut, error) {
	var f watchFilter
	if len(repos) > 0 {
		f.dirs = map[string]bool{}
		for _, name := range repos {
			r, err := m.ws.byName(name)
			if err != nil {
				return nil, watchOut{}, err
			}
			f.dirs[r.Dir] = true
			if pulls != nil {
				if err := r.Store.VerifyRemote(pulls.remote); err != nil {
					return nil, watchOut{}, fmt.Errorf("%s: %w", r.Name, err)
				}
				pulls.repos = append(pulls.repos, r)
			}
		}
	} else if pulls != nil {
		// Unscoped, it pulls the served repositories that have the remote.
		for _, r := range m.ws.list() {
			if r.Store.VerifyRemote(pulls.remote) == nil {
				pulls.repos = append(pulls.repos, r)
			}
		}
		if len(pulls.repos) == 0 {
			return nil, watchOut{}, fmt.Errorf("no served repository has a remote named %s", pulls.remote)
		}
	}
	if label != "" {
		f.label = issuelib.NormalizeLabel(label)
	}
	if len(ids) > 0 {
		f.ids = map[string]bool{}
		for _, id := range ids {
			_, xidr, err := m.ws.find(id)
			if err != nil {
				return nil, watchOut{}, err
			}
			f.ids[xidr] = true
		}
	}
	timeout := defaultWatchTimeout
	if secs > 0 {
		timeout = min(time.Duration(secs)*time.Second, maxWatchTimeout)
	}
	out, err := m.waitFor(ctx, f, since, timeout, pulls)
	if err != nil {
		return nil, watchOut{}, err
	}
	var text strings.Builder
	for _, n := range out.Pulls {
		text.WriteString(n.line() + "\n")
	}
	for _, ch := range out.Changes {
		text.WriteString(ch.oneLine() + "\n")
	}
	if len(out.Changes) == 0 && len(out.Pulls) == 0 {
		fmt.Fprintf(&text, "No change in %s\n", timeout)
	}
	fmt.Fprintf(&text, "Cursor: %s\n", out.Cursor)
	return result(text.String()), out, nil
}

// pullDue pulls the remote into each repository not pulled from it within
// -fetch, by any watch, and looks at once when it pulled, so what it brought
// is logged before the watch reads the log. A repository being pulled by
// another watch is not waited for: what that pull brings reaches every watch
// by the look.
func (m *mcpServer) pullDue(ctx context.Context, pulls *watchPulls) []pullNote {
	var did []pullNote
	pulled := false
	for _, r := range pulls.repos {
		p := m.pullerFor(r, pulls.remote)
		if !p.due(m.fetch) {
			continue
		}
		if d, ok := p.pull(); ok {
			did = append(did, d...)
			pulled = true
		}
	}
	if pulled {
		m.look(ctx)
	}
	return did
}

// pullerFor is the puller of a repository and remote, one per pair for the
// server's life.
func (m *mcpServer) pullerFor(r *repo, remote string) *puller {
	m.pullMu.Lock()
	defer m.pullMu.Unlock()
	key := r.Dir + " " + remote
	p := m.pullers[key]
	if p == nil {
		p = newPuller(r.Store, "", remote, pullTimeout)
		m.pullers[key] = p
	}
	p.named(repoLabel(m.ws, r))
	return p
}

// standing is what the pullers of pulls left standing that f wants.
func (m *mcpServer) standing(pulls *watchPulls, f watchFilter) []pullNote {
	var out []pullNote
	for _, r := range pulls.repos {
		dir := r.Dir
		out = append(out, m.pullerFor(r, pulls.remote).stands(func(xidr string) bool { return f.wants(dir, xidr) })...)
	}
	return out
}

// waitFor answers the changes after since that f matches, waiting for one
// until the timeout. Without since it starts from the latest change found.
//
// With pulls it pulls before it starts -- so what that brings, on a first
// call, is where the watch begins -- and every -fetch while it waits. It
// answers too when what stands -- refusals, failures -- is not what the
// cursor says the watch was last told, with all that stands now, or clear.
func (m *mcpServer) waitFor(ctx context.Context, f watchFilter, since string, timeout time.Duration, pulls *watchPulls) (watchOut, error) {
	var did []pullNote
	var fetchC <-chan time.Time
	if pulls != nil {
		did = m.pullDue(ctx, pulls)
		t := time.NewTicker(m.fetch)
		defer t.Stop()
		fetchC = t.C
	}
	m.mu.Lock()
	cursor, began, told := m.seq, time.Now(), ""
	if since != "" {
		seq, b, h, err := m.parseCursor(since)
		if err != nil {
			m.mu.Unlock()
			return watchOut{}, err
		}
		if len(m.log) > 0 && seq+1 < m.log[0].seq {
			m.mu.Unlock()
			return watchOut{}, fmt.Errorf("cursor %s is older than the %d changes this server keeps; call without since, and issue_list to catch up", since, watchLogCap)
		}
		cursor, began, told = seq, b, h
	}
	m.mu.Unlock()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		m.mu.Lock()
		var events []watchEvent
		for _, ev := range m.log {
			if ev.seq > cursor {
				events = append(events, ev)
			}
		}
		cursor = m.seq
		wake := m.wake
		m.mu.Unlock()
		var alarms []pullNote
		hash := told
		if pulls != nil {
			stands := m.standing(pulls, f)
			if hash = alarmsHash(stands); hash != told {
				alarms = stands
				if len(stands) == 0 {
					alarms = []pullNote{{Remote: pulls.remote, What: clear}}
				}
			}
		}
		out := watchOut{Changes: []watchChange{}, Cursor: m.cursor(cursor, began, hash)}
		for _, ev := range coalesce(events) {
			if !f.wants(ev.repo.Dir, ev.xidr) {
				continue
			}
			ch, ok := describe(ev.repo.Store, repoLabel(m.ws, ev.repo), ev.xidr, ev.was, ev.now, began)
			if ok && f.matches(ch) {
				out.Changes = append(out.Changes, ch)
			}
		}
		out.Pulls = append(alarms, withChanges(did, out.Changes)...)
		if len(out.Changes) > 0 || len(alarms) > 0 {
			return out, nil
		}
		select {
		case <-fetchC:
			did = append(did, m.pullDue(ctx, pulls)...)
		case <-wake:
		case <-deadline.C:
			return out, nil
		case <-ctx.Done():
			return watchOut{}, ctx.Err()
		}
	}
}

// coalesce makes the events for one issue in one repository into one, from
// the first's refs to the last's, in the order each issue first changed: an
// answer says what happened to an issue since the cursor, once -- a close
// found half done by one look and finished by the next is one close.
func coalesce(events []watchEvent) []watchEvent {
	var out []watchEvent
	at := map[string]int{}
	for _, ev := range events {
		key := ev.repo.Dir + " " + ev.xidr
		if i, ok := at[key]; ok {
			out[i].now, out[i].seq = ev.now, ev.seq
			continue
		}
		at[key] = len(out)
		out = append(out, ev)
	}
	return out
}

// A cursor is the server's epoch, a change's number, when the watch began --
// the first call, without since -- and what the watch was last told stands
// (alarmsHash). Numbers start again with each server, so a cursor from
// another server is refused rather than read as this one's; the start carries
// through the calls, so an issue new to the watch lists only what was done to
// it since; and what stands is said when it changes, not on every call.
func (m *mcpServer) cursor(seq uint64, began time.Time, told string) string {
	return m.epoch + "." + strconv.FormatUint(seq, 10) + "." + strconv.FormatInt(began.Unix(), 10) + "." + told
}

// parseCursor answers a cursor's change number, start and what it was told
// stands; m.mu is held.
func (m *mcpServer) parseCursor(c string) (uint64, time.Time, string, error) {
	bad := fmt.Errorf("cursor %q is not one issue_watch answered", c)
	parts := strings.Split(c, ".")
	if len(parts) != 4 {
		return 0, time.Time{}, "", bad
	}
	seq, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return 0, time.Time{}, "", bad
	}
	began, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0, time.Time{}, "", bad
	}
	if parts[0] != m.epoch || seq > m.seq {
		return 0, time.Time{}, "", fmt.Errorf("cursor %s is from another server; call without since", c)
	}
	return seq, time.Unix(began, 0), parts[3], nil
}
