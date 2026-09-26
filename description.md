# git-issue mcp: an issue can be subscribed to, but an agent asked to subscribe cannot — the subscription is the host's, and no tool offers it

`git issue mcp` offers each issue as a resource a host can subscribe to (docs/mcp.md: "offers each
issue as a resource a host can subscribe to"). But subscribing is a HOST act (`resources/subscribe`),
and the host decides whether its agent can do it. Claude Code gives the agent `ListMcpResources`,
`ReadMcpResource` and `ReadMcpResourceDir`, and no subscribe. So when a person asks the agent to
"use the MCP issue subscription", the agent cannot: the capability the server advertises is out of
reach of the one party asked to use it, and the fallback is the agent polling a resource on a timer.

Seen 2026-09-26 in a Claude Code session on the verse repository (git-issue v0.2.2, served from
the repo's `.mcp.json` and `~/.config/git-issue.tony`):

```
❯ are you watching for issue updates?

No. Nothing in this session is watching the issues. The git-issue MCP server says it offers each
issue as a resource a host can subscribe to, but I haven't subscribed to any, and I don't know
whether this client would deliver updates to me if I did. I also haven't set up any polling. I'd
only see a change by reading the issue again.

If you want me to follow one, for example w22rnvsj or the verse/ask branch issue 7z2rz9p9, I can
check it periodically: pull the issues and report only when an issue changes (a new comment, an
edit, or a state change). Should I, and which ones?

❯ use the MCP issue subscription for 7z2rz9p9

⏺ readMcpResource(Read resource "issue://7z2rz9p9h12krc2qpsn0/meta" from server "git-issue")
  … +148 lines

I can't subscribe from here. This client only lets me list and read MCP resources:
ListMcpResourcesTool, ReadMcpResourceTool and ReadMcpResourceDirTool. There's no subscribe call, so
the server's issue subscriptions have nothing to deliver to. I read
issue://7z2rz9p9h12krc2qpsn0/meta once, so here's where it stands now:

- Last updated 2026-09-26 01:45, with two comments: all steps built (at b86f9cac), then the
  runtime-raised turn (four more commits, at cd3018bf, ten commits from main).
- The status comment says "Not merged, not pushed". I haven't checked whether the branch has been
  pushed since.

The nearest thing to a subscription is polling that resource in the background: re-read its
updated time every few minutes and tell you when it changes, with the new comment or the edit.
Should I set that up, and at what interval? The issue changes on the scale of hours, so every
15–30 minutes would be enough.
```

## What would fix it

The watch the server already runs for subscriptions reaches an agent only if the host passes it
through. Offering it as a TOOL puts it in the agent's hands in any host:

- **A blocking watch tool** (`issue_watch`, say): given ids (or a label, or the open list) and a
  timeout, it answers when one of them changes — a new comment, an edit, a state change — with
  what changed, or with nothing at the timeout. An agent runs it in the background and is woken by
  the answer; no host subscription needed, and no fixed polling interval.
- **Or a cheap change probe** (`issue_changed_since`, taking the `updated` time or ref an agent last
  saw) if a blocking call does not fit a host's tool model. That keeps polling honest: one small
  answer per poll rather than a whole resource read.

Either way docs/mcp.md should say which hosts deliver resource subscriptions to their agent, since
"a host can subscribe" reads as "you can ask your agent to subscribe", and in Claude Code you
cannot.