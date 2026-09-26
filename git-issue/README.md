# git-issue

Issues that live in the repository, as git refs, and travel the way code does.

## The model

- **An issue is a ref in your repository.** Not a file in the tree, not a row
  somewhere else. Clone the repository and pull its issues, and you have them all.
- **Every clone is complete.** You create, comment, label and close offline, with
  nothing to reach and nothing to sign in to. There is no server.
- **Ids never collide.** Each is minted locally, so two people filing issues on a
  plane land two issues, not one.
- **Sync is push and pull, like branches.** Each side keeps what the other
  added. Two clones that changed one issue are merged; what cannot be merged
  is named, never overwritten.
- **History is git history.** Every change is a commit on the issue's ref, so
  `git log` on it is the issue's audit trail, and nothing is ever rewritten.

That is the whole of it. Everything else here is a command over those refs.

## Install

```bash
go install github.com/signadot/tony-format/git-issue@latest
```

The binary is called `git-issue`, which is how `git issue ...` reaches it.

## Use

An issue is named by a 20-character id; every command takes any unambiguous
prefix of one, so you type three or four characters.

```bash
git issue create "Fix the thing" --body "What is wrong, and what done looks like"
git issue list                       # open issues, newest first
git issue show j2dz                  # by prefix: description, discussion, links
git issue comment j2dz "Root cause: the cache races"
git issue label j2dz bug
git issue close j2dz --commit HEAD   # closed by the commit that fixed it
git issue push                       # to origin; pull brings others' issues here
```

Closing moves the issue's ref rather than rewriting it, so the id keeps
resolving after. The full command reference is
[docs/commands.md](docs/commands.md).

## For an agent

`git issue mcp` serves the tracker to an agent's host over MCP:

```bash
claude mcp add git-issue -- git issue mcp
```

It serves the repositories in `~/.config/git-issue.tony` and the one it was
started in, finds any issue by its id across them, and offers each issue as a
resource a host can subscribe to. See [docs/mcp.md](docs/mcp.md).

## More

| | |
|---|---|
| [docs/model.md](docs/model.md) | the distributed model, one page |
| [docs/commands.md](docs/commands.md) | every subcommand, with its flags and rules |
| [docs/mcp.md](docs/mcp.md) | the MCP server: working set, tools, resources, the watch |
| [docs/ext.md](docs/ext.md) | ext references: another repository's issue, mirrored here |
| [docs/storage.md](docs/storage.md) | the refs, the issue tree, `meta.tony`, ids |
| [docs/workflows.md](docs/workflows.md) | a feature, a bug, an umbrella, end to end |
| [docs/design.md](docs/design.md) | the design as built, and why |

Go package docs: [issuelib](https://pkg.go.dev/github.com/signadot/tony-format/git-issue/issuelib)
(the store), [ops](https://pkg.go.dev/github.com/signadot/tony-format/git-issue/ops)
(the operations), [commands](https://pkg.go.dev/github.com/signadot/tony-format/git-issue/commands)
(the CLI and the server).

## License

See the parent project's license.
