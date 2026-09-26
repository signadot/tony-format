# git-issue for agents: an umbrella -- ext references, several repositories in one server with a working set, and issue:// resources that watch, closing on a scenario

## Why

git-issue is where an agent's work is planned, recorded and closed, and the agent reaches it
by shelling out. `git issue mcp` (1k3x9sp6h12ks5c0pxn0, done) made it a server; in practice
the server is not yet usable for the work it is for, which crosses repositories in one
session: it serves one repository, from inside it, and knows nothing of another. What is
missing is a short list, and this issue is that list, in the order it lands, with what done
looks like.

## The plan

1. **ext references** (a3v2a4j6h12kse4ypxn0). Another repository's issue mirrored into this
   one under `ext/<source>/<xidr>`, read-only, refreshed by pull and carried by push, so a
   relation across repositories resolves from one repository alone. First, because the
   multi-repository server's cross-repository relate is built on it, and because it is a
   change to the store that the rest only uses.
2. **Several repositories in one server, with a working set** (7qfhwth7h12ksxcwpxn0).
   `GitStore` takes a directory; the server serves a set that comes from `-C`, from
   `~/.config/git-issue.tony`, and from the repository it was started in; `repo_add`,
   `repo_remove` and `repo_list` change it while it runs, in memory and, on request, in the
   config; an id resolves across the set, `create`/`list`/`push`/`pull` take `repo`, and
   `relate` across the set mirrors then relates. The server is a view and holds nothing; the
   set is the user's configuration.
3. **`issue://` resources, and watching** (eg8zmb1sh12ksr48pxn0). An issue and the open list
   as resources a host reads into context, a template with completion on the id, and
   subscriptions that hear a change -- from the server's own tools, AND from a poll of the
   repositories' `refs/git-issues/` tips, so a comment made from a shell beside the server,
   or a pull from another clone, is heard too. Without the poll, "watching" would be a word.

## Done

The umbrella closes when each of the three has, and this scenario runs, as a test where it
can be one and by hand where it cannot:

- A host starts `git issue mcp` from a home directory, nowhere in particular, and the server
  serves the working set from `~/.config/git-issue.tony`.
- The agent `repo_add`s verse, `issue_create`s an issue there, and `issue_relate`s it to an
  issue in tony-format; the relation resolves from a fresh clone of verse alone, as an ext
  reference.
- The agent `issue_edit`s the verse issue, and `issue_close`s it with the commit that made the
  change; `issue_for_commit` on that commit finds it.
- A host subscribed to `issue://<that id>` hears a `git issue comment` made from a shell,
  within the poll's interval.
- `issue_push` sends the verse issue and its ext reference to verse's origin, and nothing to
  tony-format's.

## Out of it

- Attachments as resources or through a tool; `serve` beyond what it is; a watch on the
  filesystem beyond the poll. Each is its own issue if wanted, and none is what makes the
  server usable.