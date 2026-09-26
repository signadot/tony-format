# git-issue: `git issue mcp`, an MCP server over the store, so an agent works the tracker as tools rather than by shelling out

## Why

An agent working in a repository that tracks its issues here reaches them by running
\`git issue ...\` in a shell and parsing what it prints. That works, and it is how every issue in
this tracker was filed, but the agent is reading a rendering meant for a person (\`show\`'s text,
\`list\`'s one-liners with colour), re-deriving the command grammar from \`-h\`, and paying a
process per call. The host (Claude Code, and anything else speaking MCP) already has a protocol
for exactly this: a server announces tools with typed parameters and descriptions, the host
gates each tool by name, and the agent calls them with structured arguments and gets structured
answers back.

\`git issue mcp\` is that server, over the same \`Store\` the commands are written against.

## Shape

**A subcommand, stdio, in the repository it is started in.** \`git issue mcp\` speaks MCP over
stdin/stdout and exits when the host closes them. It is started by the host, not by a person:

    claude mcp add git-issue -- git issue mcp          # or, in .mcp.json:
    { "mcpServers": { "git-issue": { "command": "git", "args": ["issue", "mcp"] } } }

The repository is the process's working directory, as it is for every other subcommand -- a host
starts a project-scoped server in the project -- with \`-C <dir>\` for a host that does not. No
listener and no address: \`serve\` binds loopback because nothing it serves authenticates, and
stdio has no such question, the host owns both ends.

**Tools, one per thing an agent does, thin over Store.** Each takes the arguments the command
takes, as named JSON parameters rather than flags, and every parameter that the CLI would open
\`\$EDITOR\` for is required inline -- a tool never reaches the editor branch.

| tool | parameters | answers |
|---|---|---|
| \`issue_list\` | \`all?\`, \`label?\` | rows: id, status, title, labels, created, updated |
| \`issue_show\` | \`id\` | meta, description, the discussion in order, attachments by name |
| \`issue_create\` | \`title\`, \`body\`, \`labels?\` | the id |
| \`issue_comment\` | \`id\`, \`text\` | the comment's path |
| \`issue_label\` | \`id\`, \`add?\`, \`remove?\` | the labels after |
| \`issue_close\` | \`id\`, \`commit?\` | |
| \`issue_reopen\` | \`id\` | |
| \`issue_link\` | \`id\`, \`commit\` | |
| \`issue_relate\` | \`id\`, \`other\`, \`kind\`: related / blocks / duplicates | |
| \`issue_for_commit\` | \`commit\` | the ids |
| \`issue_pull\`, \`issue_push\` | \`remote?\`, \`ids?\`, \`dry_run?\`, \`force?\` | the report, per issue |

\`id\` is a full XIDR or any unambiguous prefix, as everywhere; an ambiguous one is refused naming
what it matched. \`issue_edit\` (title, body) belongs here too and is the same missing surface
dcp201s6h12krnmdpnn0 asks the CLI for: one operation, two front ends.

Not exposed: \`serve\`, \`migrate\`, \`migrate-comments\`, \`export\`/\`import\`, and \`attach\` (a
path on the agent's disk is a different question from a body in a call; it can come later as a
tool taking content).

**Push and pull are tools of their own, not options.** A host gates by tool name, so a
deployment that wants an agent reading and writing locally but never touching the remote
denies two names. Both take \`dry_run\`, as the commands do, so an agent can ask before acting.

**Answers are structured, and the text a person would see.** A tool answers \`structuredContent\`
-- the \`Issue\` struct as JSON, through gomap, plus the description and comments -- and a text
block that is what the command prints, so a host that shows tool output to a person shows
the familiar thing. A refusal the store makes (compare-and-swap on a racing write, a merge it
cannot make, an unknown id) is a tool error carrying the store's message, never a protocol
error: the agent reads why, as a person reads it from the CLI.

**The tool descriptions carry the tracker's rules.** They are what the agent reads instead of
a CLAUDE.md: \`issue_close\` says a fix closes with the commit that made it; \`issue_create\` says
a change gets an issue and its commit carries \`Issue: <id>\`; \`issue_push\` says what a lease
refusal means. The rules live once, next to the operation they govern.

## What it takes

1. **One implementation per operation.** Today each command's logic is in its \`run\`: \`close\`
   verifies the commit and writes the message, \`relate\` keeps Blocks/BlockedBy as a pair,
   \`label\` normalizes and replaces a key's value. Two front ends over that would drift. So each
   becomes a function over \`Store\` -- \`ops.Close(store, id, commit)\`, in a package the commands
   and the server both import -- and the CLI's \`run\` parses flags and calls it. That is most of
   the work, and it is worth doing whether or not the server ships: it is what makes the
   commands testable without a \`cli.Context\`.
2. **The protocol.** The official Go SDK (\`github.com/modelcontextprotocol/go-sdk\`) handles
   initialize, capability negotiation, \`tools/list\`, \`tools/call\`, schema generation from
   parameter structs, and the stdio transport. The alternative is hand-rolling the JSON-RPC
   framing and those four methods, which is a few hundred lines and a spec version to track by
   hand. Recommendation: the SDK -- \`serve\` already accepts net/http for a smaller reason -- but
   go.mod has been kept small on purpose, and this is the decision to make before writing it.
3. **Tests, in-process.** The SDK gives an in-memory transport, so a test drives the server as a
   host would -- list tools, call each one -- against a scratch repository, the way the command
   tests use \`testRepo\` and \`pushTestRepo\`. The push/pull tools are tested through the two-clone
   arrangement \`sync_test.go\` already has.
4. **Docs.** The README gets the host configuration above and the tool table; \`git issue mcp -h\`
   says what it speaks and that it is for a host to start.

## Later, not now

- **Resources.** MCP also has resources, which fit reading: \`issue://<id>\` for an issue, the open
  list as the root. A host that pulls resources into context would want them. Tools first,
  because they are what an agent acts through.
- **Notifications.** A pull that changed issues could notify the host that the resource list
  changed. Needs resources first.
- **A watch.** Nothing here is long-lived; an agent that wants to know when an issue changes
  polls \`issue_show\`. The tracker is refs in a repository, so there is no event to subscribe to
  short of a filesystem watch.