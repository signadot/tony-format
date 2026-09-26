# git-issue documentation

git-issue keeps a repository's issues in the repository itself, as git refs,
and every clone is a complete tracker. The [README](../README.md) is enough to
install it and use it; these pages are the rest, one per thing a reader looks for.

- [The distributed model](model.md) -- the one idea, on one page: an issue is a
  ref, every clone is complete, ids never collide, sync is push and pull, and
  history is git history.
- [Commands](commands.md) -- every subcommand: what it takes, what it writes, and
  the rules it follows (labels, relations, sync, export and import, migrations).
- [The MCP server](mcp.md) -- `git issue mcp`: the working set of repositories it
  serves, the tools, the `issue://` resources, and the watch that hears changes
  made beside it.
- [Ext references](ext.md) -- another repository's issue mirrored here, read-only,
  so a relation across repositories resolves from one repository alone.
- [Storage](storage.md) -- the ref namespaces, the tree under an issue's ref,
  `meta.tony`, and why ids look the way they do.
- [Workflows](workflows.md) -- a feature, a bug and an umbrella, end to end.
- [Design](design.md) -- the tracker as built, the reasoning behind the parts that
  are not obvious, its limits, and what is not done.
