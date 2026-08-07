# git-issue design

This is the design of the tracker as built, and the reasoning behind the parts
that are not obvious. For how to use it, see [README.md](README.md); for the API,
see the package docs in `issuelib` and `commands`.

## Goals

1. Track issues, tasks and decisions that span commits and branches
2. Keep an issue accessible after it closes, at the same name
3. Link issues to commits, in both directions
4. Organize discussion and artifacts per issue
5. No external service: everything lives in git

## Non-goals

- Bridges to other trackers (GitHub, GitLab)
- Milestones, sprints, assignment
- Full-text search — `git log` and `git grep` reach the objects
- A writable web UI. `git issue serve` is a read-only viewer, and read-only is a
  consequence of the sync model rather than a stage on the way to something else;
  see [Sync](#sync-and-what-it-costs).

## Storage model

An issue is a git ref pointing at a commit chain. The ref namespace carries the
status:

```
refs/issues/<xidr>    an open issue
refs/closed/<xidr>    a closed issue
refs/notes/issues     reverse index, commit -> issue IDs
```

Each operation appends a commit whose tree is the whole issue:

```
refs/issues/j2dzt7xph12kswa9esn0
  |
  create: issue j2dzt7xph12kswa9esn0
  |   description.md, meta.tony
  |
  label: added bug, urgent
  |   meta.tony updated
  |
  link: c477908
  |   meta.tony updated
  |
  comment: a comment
      discussion/20260807T211114Z-4ca263f7.md added
```

`meta.tony` is generated from the `Issue` struct in `issuelib`, so the on-disk
schema and the Go type cannot drift; `description.md` and the discussion are
markdown, written and read by people.

## Decisions

### Refs, not files in the working tree

An issue tracked as a file in the tree would be a file in every diff, every
merge, and every `git status` — issue edits would collide with code review, and
checking out an old commit would take the issues back in time with it. Under
`refs/`, issues are in the repository, clone with it and travel over the same
transport, but they are not in the working tree and never appear in a code diff.

The cost is that issues do not clone by default: `git clone` fetches `refs/heads`
and `refs/tags`, so a new clone starts with no issues until `git issue pull`.

### A commit chain per issue

Every mutation appends a commit rather than rewriting the tip, so
`git log refs/issues/<xidr>` is the issue's history for free — who changed what,
when — with no separate event log to keep consistent with the state. Commit
messages name the operation (`link: c477908`, `label: added bug, urgent`), which
makes the log readable without tooling.

### Status is the namespace

Closing moves the ref from `refs/issues/` to `refs/closed/`; the commit chain is
untouched. Status could have been a field in `meta.tony` alone, but then listing
open issues would mean reading every issue's metadata, and the field is written
too. It is written — `meta.tony` has `status` — but where the ref lives is what
listings believe, because that is what a ref scan can answer cheaply and what
cannot disagree with itself.

Because both namespaces are searched on every lookup, an ID keeps resolving after
the issue closes. A link handed to someone does not rot when the issue is
finished.

### XIDR, not a sequential counter

The original design allocated six-digit IDs from `refs/meta/issue-counter`,
incremented under git's atomic ref update. That is correct within one repository
and wrong across clones: two people filing an issue offline both allocate
`000042`, and on sync there is no way to tell the two issues apart — same ID,
different content, and force-push means one simply disappears.

An XID is 12 bytes — timestamp, machine, process, counter — so it needs no
coordination. An XIDR is those bytes reversed, and the reversal is the point:
counter and machine bytes come first, so the three or four characters a person
types are already the part that varies. Unreversed, every issue filed in the same
second shares its leading characters and no short prefix resolves.

Prefix lookup is therefore the normal way to name an issue, and an ambiguous
prefix is an error rather than a guess.

### Content-addressed comment names

Comments were `discussion/001.md`, `002.md`, numbered by counting the files
already present. Two clones each adding a comment both wrote `003.md`; sync kept
one and dropped the other, silently, because both sides had a file at that path.
The count also skewed whenever an attachment was present.

The name is now `discussion/<timestamp>-<hash>.md`, where the hash is over the
comment's own bytes. Two different comments cannot collide; two identical ones
collapse, which is the right answer. The timestamp keeps names sorting
chronologically, but `show` sorts on the timestamp *inside* each comment, so a
renamed or hand-written file still lands in the right place.

`git issue migrate-comments` converts the old names. It appends a commit rather
than rewriting history, but that commit replaces the issue's tree wholesale, so
it is dry-run by default and stashes each ref it touches under
`refs/issue-backup/<ts>/` first.

### Labels in `meta.tony`, not `labels.tony`

The original design gave labels their own file. A second file means a second
parse, a second write path, and two documents that can disagree about which issue
they belong to. Labels are a small list of short strings — they belong in the
metadata document with everything else, and `list --label` gets them from the
same read that produced the issue.

### Plumbing over a temporary index

Writes go through `hash-object`, `mktree`/`update-index`, `commit-tree` and
`update-ref`, with `GIT_INDEX_FILE` pointed at a temporary file. Nothing touches
the caller's index or working tree, so filing an issue in the middle of a messy
edit is safe, and no operation needs a clean tree to run.

### Read-only `serve`

`serve` exists because nothing else in the tool can hand someone a link. It reads;
it does not write. See [Sync](#sync-and-what-it-costs) — a second writer racing
the CLI would silently drop whichever update lost, and no amount of care in the
HTTP layer fixes that. Markdown is rendered with goldmark's default (safe)
renderer, since issue text is attacker-controlled in the sense that matters:
served from the same origin as everything else.

There is no authentication and should not be; the default bind is loopback.

## Sync, and what it costs

`push` and `pull` move refs with force refspecs:

```
+refs/issues/*:refs/issues/*
+refs/closed/*:refs/closed/*
+refs/notes/issues:refs/notes/issues
```

Force is what makes the model work at all: two clones that both edited an issue
have divergent chains, and without force neither could push. With it, the last
writer wins — the loser's commits remain in the object store but drop off the
ref, recoverable only by someone who knows to go looking in the reflog.

The same applies in reverse, which is the part that surprises people: `pull`
force-fetches, so a local issue that diverged from the remote is reset to the
remote's version. Comment locally, don't push, then pull, and the comment is off
the ref.

For the way this is used — one person editing an issue at a time, pushing when
they are done — that has been acceptable. It is still the single largest thing
wrong with the design, and everything else marked "read-only" or "one at a time"
in this document is downstream of it.

The notes ref is the sharper edge: `refs/notes/issues` is one ref for the whole
repository, so force-pushing it replaces the remote's entire reverse index. A
link made in another clone and not yet fetched is dropped from the remote ref by
the next `push --all`. The issue itself still lists the commit — only the
commit → issue direction is lost, and `git issue link` again restores it.

Fixing this properly means real merge semantics for issue refs: a three-way merge
over `meta.tony` (the list fields union cleanly; `status` and `closed_by` need a
rule) and a union merge for notes, which git already supports via
`notes.mergeStrategy`. That work would also unlock a writable `serve`.

`pull` does one small repair: an issue that arrives in both namespaces is
resolved by keeping whichever ref has more history, or the closed one if neither
descends from the other.

## Designed but not built

From the original design, still absent:

- **Commit-message integration.** `Closes #NNN` / `See #NNN` parsed by a
  `post-commit` hook. Nothing installs a hook and nothing parses commit messages;
  linking and closing are explicit commands. The XIDR makes the syntax less
  attractive than it looked with short numeric IDs.
- **Branch linking.** `meta.tony` has a `branches` field and `show` prints it,
  but no command writes it.
- **`git issue discuss`.** Split into `comment` (text) and `attach` (files), which
  are different enough operations to want different arguments.
- **Merging issue refs.** The original design assumed "issue refs merge like
  branches (git handles this)". They do not: git can merge them, but nothing
  invokes it, and the transport is force-push. See above.

## Changes from the original design

| Original | Now | Why |
|---|---|---|
| `refs/issues/001`, counter ref | `refs/issues/<xidr>` | counters cannot be allocated offline |
| `labels.tony` | `labels` in `meta.tony` | one document, one write path |
| `discussion/<date>-<topic>.md`, hand-named | `discussion/<ts>-<hash>.md` | names that cannot collide on sync |
| `git issue discuss` | `comment`, `attach` | different inputs, different commands |
| `git issue label --remove` | `git issue unlabel` | reads better, parses simpler |
| Hook-driven `Closes #NNN` | explicit `close`, `link` | never built |
| "Web UI: non-goal" | read-only `serve` | a link to an issue is worth having |

## Known defects

- **No merge for issue refs.** The one described above, and the root of most of
  the rest.
- **`git issue migrate` is not idempotent.** It re-identifies every issue it
  finds rather than only the legacy-numeric ones, so a second run mints fresh
  XIDRs for issues that already had them and every ID recorded elsewhere stops
  resolving. Filtering on `IsLegacyRef` would fix it.
- **Colors are unconditional.** Listings write ANSI escapes whether or not the
  output is a terminal, so piping to a file captures them.
- **One repository per process.** `GitStore` drives the git binary in the
  process's working directory and holds no path of its own, which is why the
  tests must not run in parallel.

## Status

Implemented and in use. `refs/meta/issue-counter` no longer allocates anything;
`push` and `pull` still carry its refspec, harmlessly, for repositories that
still have the ref.
