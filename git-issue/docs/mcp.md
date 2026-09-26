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
server holds nothing, and every issue stays in its repository.

When more than one is served, `issue_create`, `issue_list`, `issue_push`,
`issue_pull` and `issue_for_commit` take `repo`; `issue_relate` across two
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
| `repo_list` | | the repositories served, and where each came from |
| `repo_add` | `path`, `persist?` | the set after |
| `repo_remove` | `repo` | the set after |

`id` is a full XIDR or any unambiguous prefix, everywhere; a prefix an id
reaches in more than one repository is ambiguous, naming each. A refusal -- an
unknown id, an issue closed twice, a merge that cannot be made -- is a tool
error carrying the message the command would have printed, never a protocol
error. Push and pull are tools of their own, so a host that wants an agent
working locally and never touching the remote denies two names; both take
`dry_run`. The tool descriptions carry the tracker's rules -- a change gets an
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

A change made through the server's own tools is announced at once. A change
made beside the server -- a comment from a shell, a pull in another clone, an
edit by another agent -- is found by the watch: every `-poll` (5s by default),
each served repository's refs are compared with the last look, an issue whose
ref moved is announced to its subscribers, and an issue that appeared or went
changes the listing. `-poll 0` turns the watch off.

Hosts differ in what they show of this. Claude Code does not surface resource
subscriptions in its UI, so the watch shows up as the next read being current
rather than as a notification.
