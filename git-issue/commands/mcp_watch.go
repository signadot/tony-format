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
	IDs     []string `json:"ids,omitempty" jsonschema:"the issues to watch: full ids or unambiguous prefixes; every issue when empty"`
	Label   string   `json:"label,omitempty" jsonschema:"only issues carrying this label after the change (a plain label, or key=value)"`
	Since   string   `json:"since,omitempty" jsonschema:"the cursor a previous issue_watch answered: changes after it are answered at once, so none is missed between calls. Without it, only changes from now on"`
	Timeout int      `json:"timeout,omitempty" jsonschema:"seconds to wait for a change before answering with none; 300 by default, at most 3600"`
}

type watchOut struct {
	Changes []watchChange `json:"changes" jsonschema:"the issues that changed, in the order the changes were found; empty at the timeout"`
	Cursor  string        `json:"cursor" jsonschema:"pass as since to the next issue_watch"`
}

func addWatchTool(m *mcpServer) {
	mcp.AddTool(m.s, &mcp.Tool{
		Name: "issue_watch",
		Description: "Wait for issues to change -- a comment, an edit, a label, a close or reopen, an issue filed or pulled -- by this " +
			"server's tools or by anything else that moves their refs (a shell, a pull, another agent), and answer what changed. " +
			"Give ids, or a label, or neither for every issue. It answers as soon as a change matches, or with none at the timeout, " +
			"and always with a cursor: pass it back as since and nothing that changed between two calls is missed. " +
			"It blocks while it waits, so call it where waiting does not hold up other work.",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in watchIn) (*mcp.CallToolResult, watchOut, error) {
		var f watchFilter
		if in.Label != "" {
			f.label = issuelib.NormalizeLabel(in.Label)
		}
		if len(in.IDs) > 0 {
			f.ids = map[string]bool{}
			for _, id := range in.IDs {
				_, xidr, err := m.ws.find(id)
				if err != nil {
					return nil, watchOut{}, err
				}
				f.ids[xidr] = true
			}
		}
		timeout := defaultWatchTimeout
		if in.Timeout > 0 {
			timeout = min(time.Duration(in.Timeout)*time.Second, maxWatchTimeout)
		}
		out, err := m.waitFor(ctx, f, in.Since, timeout)
		if err != nil {
			return nil, watchOut{}, err
		}
		var text strings.Builder
		for _, ch := range out.Changes {
			text.WriteString(ch.oneLine() + "\n")
		}
		if len(out.Changes) == 0 {
			fmt.Fprintf(&text, "No change in %s\n", timeout)
		}
		fmt.Fprintf(&text, "Cursor: %s\n", out.Cursor)
		return result(text.String()), out, nil
	})
}

// waitFor answers the changes after since that f matches, waiting for one
// until the timeout. Without since it starts from the latest change found.
func (m *mcpServer) waitFor(ctx context.Context, f watchFilter, since string, timeout time.Duration) (watchOut, error) {
	m.mu.Lock()
	cursor := m.seq
	if since != "" {
		seq, err := m.parseCursor(since)
		if err != nil {
			m.mu.Unlock()
			return watchOut{}, err
		}
		if len(m.log) > 0 && seq+1 < m.log[0].seq {
			m.mu.Unlock()
			return watchOut{}, fmt.Errorf("cursor %s is older than the %d changes this server keeps; call without since, and issue_list to catch up", since, watchLogCap)
		}
		cursor = seq
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
		out := watchOut{Changes: []watchChange{}, Cursor: m.cursor(cursor)}
		for _, ev := range coalesce(events) {
			ch := describe(ev.repo.Store, repoLabel(m.ws, ev.repo), ev.xidr, ev.was, ev.now)
			if f.matches(ch) {
				out.Changes = append(out.Changes, ch)
			}
		}
		if len(out.Changes) > 0 {
			return out, nil
		}
		select {
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

// A cursor is the server's epoch and a change's number: numbers start again
// with each server, so one from another server is refused rather than read as
// this one's.
func (m *mcpServer) cursor(seq uint64) string {
	return m.epoch + "." + strconv.FormatUint(seq, 10)
}

// parseCursor answers a cursor's change number; m.mu is held.
func (m *mcpServer) parseCursor(c string) (uint64, error) {
	epoch, n, ok := strings.Cut(c, ".")
	seq, err := strconv.ParseUint(n, 10, 64)
	if !ok || err != nil {
		return 0, fmt.Errorf("cursor %q is not one issue_watch answered", c)
	}
	if epoch != m.epoch || seq > m.seq {
		return 0, fmt.Errorf("cursor %s is from another server; call without since", c)
	}
	return seq, nil
}
