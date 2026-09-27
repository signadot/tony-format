# git-issue: the commands use the working set, as the MCP server does

`~/.config/git-issue.tony` names the repositories a person works in, and only `git issue mcp` reads it. The server, started anywhere, finds any issue by its id across them. The commands know only the working directory: from `~/Dev/github.com/signadot`, which holds four configured clones, every command refuses (eahqxymwh12ks32vq1n0 made the refusal say why, and kept it).

## What

The set is the configured repositories and the one the command is run in, as it is for the server.

- `--repo <name>` names the repository a command runs on, by its name in the set or its directory.
- A command given an id -- show, edit, comment, attach, close, reopen, label, unlabel, link, export, push <id>, ext remove -- runs on the repository that holds the issue. Run inside a repository that holds it, that is this one, as now; otherwise the set is searched.
- A command given none -- create, pull, push, for-commit, import, ext add/fetch/refresh/list, serve -- runs on the repository it is in. Outside one it needs `--repo`, and says which there are; a set of one needs none.
- `list` and `watch` outside a repository cover every repository of the set, each line saying which.
- relate, blocks and duplicate between issues of two repositories mirror the far issue into the near one first, as issue_relate does.
- migrate and migrate-comments stay with the working directory.
- Outside a repository with no set, a command refuses as it does now.