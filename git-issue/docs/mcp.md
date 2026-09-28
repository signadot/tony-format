# The MCP server

`git issue mcp` speaks MCP over stdin and stdout. It is for a host to start,
not a person: the host gates it tool by tool, and the agent calls the tools
with typed arguments and reads structured answers -- with the text the command
would have printed alongside. There is no listener and no address.

```bash
claude mcp add git-issue -- git issue mcp
```

or, in a project's `.mcp.json`:

```json
{ "mcpServers": { "git-issue": { "command": "git", "args": ["issue", "mcp"] } } }
```

## The working set

The server serves one repository or several, and finds, for any id a tool is
given, the repository that holds it -- an id is unique across repositories. The
set is every repository these three name, together:

- `-C <dir>`, repeatable: `git issue mcp -C ~/src/tony-format -C ~/src/verse`
- `~/.config/git-issue.tony` (`$XDG_CONFIG_HOME/git-issue.tony` when set):

  ```tony
  repos:
  - /Users/you/src/tony-format
  - /Users/you/src/verse
  ```

  Full paths: nothing expands `~`.
- the repository the server was started in.

A repository named twice is served once. With none of the three the server
refuses and says so. `repo_add`, `repo_remove` and `repo_list` change and show
the set while the server runs; `repo_add` with `persist` records the repository
in the config file too. The set is your configuration, as remotes are: the
server holds nothing, and every issue stays in its repository. The commands
use the same file ([which repository](commands.md#which-repository)).

A repository is its root: one named by a directory inside it is the
repository, and is served once.

When more than one is served, `issue_create`, `issue_list`, `issue_push`,
`issue_pull` and `issue_for_commit` take `repo`, and `issue_watch` a list of
them; `issue_relate` across two
repositories mirrors the far issue into the near one as an
[ext reference](ext.md) first, so the relation resolves from that repository
alone. An issue that is in two repositories -- its own, and one mirroring it --
resolves to its own.

## Tools

| tool | takes | answers |
|---|---|---|
| `issue_list` | `repo?`, `all?`, `label?` | id, repo, status, title, labels, created, updated; newest first |
| `issue_show` | `id` | the issue whole: title, body, labels, commits, relations, the discussion in order, attachments |
| `issue_create` | `repo?`, `title`, `body`, `labels?` | the id |
| `issue_edit` | `id`, `title?`, `body?` | |
| `issue_comment` | `id`, `text` | the comment's path |
| `issue_label` | `id`, `add?`, `remove?` | the labels after |
| `issue_close` | `id`, `commit?` | |
| `issue_reopen` | `id` | |
| `issue_link` | `id`, `commit` | the full SHA |
| `issue_relate` | `id`, `other`, `kind` (related, blocks, duplicate) | whether anything changed, and what was mirrored |
| `issue_for_commit` | `repo?`, `commit` | the issues its note names |
| `issue_pull` | `repo?`, `remote?`, `force?`, `dry_run?` | the sync report |
| `issue_push` | `repo?`, `remote?`, `id?`, `force?`, `dry_run?` | the sync report |
| `issue_watch` | `repo?`, `ids?`, `label?`, `since?`, `timeout?` | each issue that changed and what was done to it, and a cursor; waits until one does |
| `issue_watch_remote` | `remote?`, and `issue_watch`'s | the same, pulling `remote` (origin) while it waits, and what the pulls did |
| `repo_list` | | the repositories served, and where each came from |
| `repo_add` | `path`, `persist?` | the set after |
| `repo_remove` | `repo` | the set after |

`id` is a full XIDR or any unambiguous prefix, everywhere; a prefix an id
reaches in more than one repository is ambiguous, naming each. A refusal -- an
unknown id, an issue closed twice, a merge that cannot be made -- is a tool
error carrying the message the command would have printed, never a protocol
error. Push and pull are tools of their own, so a host that wants an agent
working locally and never touching the remote denies their names, and
`issue_watch_remote`'s; push and pull take `dry_run`. The tool descriptions carry the tracker's rules -- a change gets an
issue and its commit carries `Issue: <id>`, a fix closes with its commit -- so
an agent reads them where the operation is.

## Resources

An issue is also something a host reads into context, rather than something
the agent calls for:

| URI | what it is |
|---|---|
| `issue://<id>` | the issue as a person reads it (`text/markdown`) |
| `issue://<id>/meta` | the issue as data, `issue_show`'s structured content (`application/json`) |
| `issue://` | the open issues of every served repository, one line each |

The open issues are listed as resources, `issue://{id}` and `issue://{id}/meta`
are templates with completion on the id, and a host that subscribes to an issue
hears it change.

## The watch

Every `-poll` (5s by default) the server compares each served repository's
refs with the last look. An issue whose ref moved is announced to its
subscribers, and an issue that appeared or went changes the listing. A tool
looks as soon as it has changed something, so with `-poll 0` only the tools'
own changes are heard. The refs are this clone's: a change pushed to a remote
is heard once a pull brings it in.

Subscribing is the host's act, not the agent's, and a host need not pass the
notifications on. Claude Code does not: its agent can read resources but not
subscribe.

`issue_watch` gives the watch to the agent. It waits until an issue changes:
one in a repository named in `repo`, one named in `ids`, one carrying `label`
before or after the change. Each scope given must match; with none, any issue
in any served repository. It answers each issue that changed with its status,
title and labels, and what was done to it: one line per commit it gained
(`comment: ...`, `label: added bug`), then `closed` or `reopened` if it moved
with no commit saying so. An issue new to the watch lists only commits made
since the watch began, or `arrived` when there are none. With no change it
answers nothing at `timeout` (300 seconds, at most 3600).

Each change says in `on` whether origin holds the issue as the change left it:
`origin`, or `local` when it does not, and the change is not pushed. It is read
from this clone's copy of the remote, as the last fetch or push left it. A push
that brings origin a change that was local answers as `pushed to origin`, one
per issue pushed. So an agent's own push comes back to it, and it should read
that as its push landing, not as news to act on.
`issue_watch_remote` answers for the remote it pulls. A repository without that
remote leaves `on` out.

Every answer carries a `cursor`. Passed back as `since`, it answers at once
what changed between two calls, so nothing is missed, and the watch began
with the first call. The server keeps the last 1024 changes, and refuses a
cursor older than those or from another server.

`issue_watch_remote` is `issue_watch` that pulls. Issues often travel by push
and pull, and a watch of this clone alone hears nothing a teammate pushed. So
it pulls `remote` (origin by default) into each repository it watches: those
named in `repo`, which must have the remote, or every served one that has it.
It pulls when it starts and every `-fetch` (30s by default) while it waits,
and never waits on a pull past its `timeout`. A repository is not looked at
while it is pulled, so a change is answered with the pull that brought it.
What a pull did to an issue comes with the change it brought, as `issue_pull`
says it, to every watch the change answers. An issue in the watch's scope
refused, or a remote not reached, stands until a person acts, and answers on
its own: all that stands, whenever it differs from what
the cursor says the watch was last told, and `clear` when nothing stands.
Each watch is told for itself, so two agents watching both hear a refusal.
A remote that does not answer within two minutes is given up on until the
next pull, and stands as `could not pull`.

It writes this clone's refs as `issue_pull` does and pushes nothing. It is a
tool of its own so that a host keeping an agent off the remote can deny it by
name, as it does `issue_pull`.

A call to either holds the agent until it answers. In Claude Code, run
[`git issue watch`](commands.md#watch) in the background under Monitor
instead; it pulls origin in the same way.
