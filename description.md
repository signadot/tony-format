# git issue serve

Add a `git issue serve` subcommand to git-issue: a read-only web UI that gives every
issue a stable, shareable URL.


git-issue's real gap is not features, it's linkability — there is no way to send
someone a link to an issue. Everything else it does, it does well from the CLI.

`serve` is READ-ONLY, and that is the whole design constraint, not an MVP shortcut.
git-issue's sync is force-refspecs in both directions (`+refs/issues/*:refs/issues/*`
in commands/push.go and commands/pull.go) with no merge anywhere in the codebase.
A second writer would be a lost-update bug, and fixing that is a separate, larger
project. Do not add write endpoints, and do not add a "just close/comment" affordance.
If you find yourself wanting one, stop and say so instead.


- cmd/git-issue/commands/ — one file per subcommand, registered in root.go's Root()
  and documented in the usageText const there. Follow the existing shape:
  cli.NewCommandAt(&cfg.Command, "serve").WithSynopsis(...).WithOpts(...).WithRun(...),
  taking issuelib.Store. See commands/show.go and commands/push.go (which uses
  cli.StructOpts for flags).
- cmd/git-issue/issuelib/ — Store interface in store.go, GitStore in git_store.go.
  Read both before starting; the interface already has everything serve needs.


- One git ref per issue. Open at refs/issues/<xidr>, closed at refs/closed/<xidr>.
  Status lives in the ref NAMESPACE, so an issue's ref changes when it closes.
- The ref's tree holds meta.tony, description.md, and discussion/<ts>-<hash>.md
  comments, plus attachments under discussion/files/. See commands/comment_id.go
  for the comment filename convention and the header-timestamp parsers already
  written there — reuse them, don't reimplement.
- Title is the first line of description.md with "# " stripped (git_store.go GetByRef).
- XIDR is 20 chars; issuelib.FindRef resolves full IDs and unique prefixes across
  BOTH namespaces. Use it — never construct refs by hand.
- meta.tony carries labels, related/blocks/blocked-by/duplicates, linked commits
  and branches. commands/show.go renders all of it for the terminal; treat it as
  the content inventory the web view should match.


Links get pasted into chat and must not rot. Requirements:
- /i/<xidr> resolves through FindRef, so a link keeps working after the issue
  closes and its ref moves namespaces.
- Prefixes accepted, but redirect to the canonical full-XIDR URL.
- / lists open issues; something like /?all=1 includes closed.
- Attachments served under a path derived from the issue, with content sniffed
  conservatively: force a download disposition rather than rendering, and never
  serve attachment bytes as text/html. An attached .html would otherwise be stored
  XSS on your own origin.


- **Markdown rendering.** go-tony's go.mod has no markdown dependency today. Either
  add one (goldmark is the obvious candidate: maintained, no transitive deps, and
  it does NOT emit raw HTML unless you pass WithUnsafe — leave that off) or render
  a deliberate subset. Pick one, say why in the commit message. Whatever you choose,
  issue text is untrusted-ish input: raw HTML in a description must not reach the page.
- **Caching.** Every Store call shells out to git. A page load forks several
  processes. GetRefCommit(ref) is a cheap validity token for an issue's entire
  content — it's both a cache key and a natural ETag. Use it or explain why not.
- **Bind address.** Default to localhost with a --addr flag. This surface has no
  authentication and shouldn't grow any.


- Table-driven tests in the style already used in cmd/git-issue (see the existing
  *_test.go files for how they set up a temp repo). Cover at minimum: prefix
  redirect to canonical URL, an issue that resolves after being closed, a comment
  with each of the two header forms, and that raw HTML in a description does not
  survive into the response.
- root.go's usageText and the Root() sub list updated.
- Any README/docs for git-issue updated to mention serve.
- gofmt clean, go vet clean, existing tests still pass.

Read the surrounding code first and match its idiom — comment density in this
package is high and the comments explain *why* (see the long one at the top of
comment_id.go). Write in that register.