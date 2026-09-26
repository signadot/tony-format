# docs: a README a person can read, the rest under docs/, and package docs fit for pkg.go.dev

## What is wrong

The README is 612 lines under 37 headings, and it is also what pkg.go.dev renders for the
module, so a person arriving either way gets everything at once and takes none of it. The
package docs are mostly good, but ops names an `mcpserver` package that does not exist, and
issuelib's storage model predates the ext and sources namespaces.

## What is needed

- A README of about 120 lines: what it is, install, the daily path (create, list, show,
  comment, close with the commit, push/pull), the agent server in a few lines, a table of
  where the rest is, license.
- `docs/`, one file per thing a reader looks for, each self-contained, with an `index.md`
  shaped for mkdocs later: `commands.md`, `mcp.md`, `ext.md`, `storage.md`, `design.md` (the
  existing one, taking the README's implementation notes, principles, limitations and future),
  `workflows.md`.
- Package docs checked with `go doc`: ops's stale reference fixed, issuelib's storage model
  brought up to date, commands' groups complete, every exported symbol with a comment that
  starts with its name.

Not this pass: a mkdocs.yml for git-issue (the site is the root one, whose docs_dir cannot
reach here without a plugin), and Go Example tests.