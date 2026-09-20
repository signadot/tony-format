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
status, under a generation that says which ref layout this is:

```
refs/git-issues/v1/open/<xidr>    an open issue
refs/git-issues/v1/closed/<xidr>  a closed issue
refs/notes/git-issues/v1          reverse index, commit -> issue IDs
```

The `v1` is there because a client names its refspecs literally, so it cannot see
or touch a ref it does not name. A client older than the generation syncs
`refs/issues/` and `refs/closed/` — gen0, below — and is therefore harmless to
anything written here. The generation changes only for a break a client of the
previous one cannot safely coexist with: a ref layout, or how sync decides what to
write. What `meta.tony` holds is not such a break, since a field added there is
read by a client that does not know it, and moving every ref in every clone for one
would be a flag day for nothing.

Each operation appends a commit whose tree is the whole issue:

```
refs/git-issues/v1/open/j2dzt7xph12kswa9esn0
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
`git log <the issue's ref>` is the issue's history for free — who changed what,
when — with no separate event log to keep consistent with the state. Commit
messages name the operation (`link: c477908`, `label: added bug, urgent`), which
makes the log readable without tooling.

### Status is the namespace

Closing moves the ref from `refs/git-issues/v1/open/` to
`refs/git-issues/v1/closed/`; the commit chain is untouched. Status could have been a field in `meta.tony` alone, but then listing
open issues would mean reading every issue's metadata, and the field is written
too. It is written — `meta.tony` has `status` — but where the ref lives is what
listings believe, because that is what a ref scan can answer cheaply and what
cannot disagree with itself.

Because both namespaces are searched on every lookup, an ID keeps resolving after
the issue closes. A link handed to someone does not rot when the issue is
finished.

### gen0, and adopting it

The layout before the generation existed — `refs/issues/<xidr>`,
`refs/closed/<xidr>`, `refs/notes/issues` — is gen0, and a client that has not been
upgraded goes on writing it. Its work is neither lost nor left where this
generation cannot see it: every read of an existing issue adopts first, moving each
gen0 ref into the current layout and folding a gen0 reverse index into the current
one with `git notes merge -s union`. So upgrading the binary in a clone full of
gen0 issues looks like nothing happening, which is the point.

Adoption is local and one-directional. Refs in this clone are renamed; nothing is
sent anywhere, and a `pull` does not migrate a remote. A ref is recreated at the
commit it was already at — no commit is rewritten — so every SHA recorded
elsewhere, every `Issue:` trailer and every id keeps resolving. That is what the
numeric-id migration, which re-identified issues, could not say for itself, and it
is why there is no migration command to run: a remote is migrated by an ordinary
push, under the rule that decides every other push.

Where both layouts hold the same issue — someone ran the old binary after the new
one — the later of the two tips wins, status included. Only a genuine divergence is
left alone, named for a person, for a sync to merge.

Reads take adoption once per process, and it must not fail the command it rides on:
a listing that cannot adopt should still list. Anything that has just fetched gen0
refs, or is about to decide from local refs, calls it directly instead — that once
may be long spent by then, and what arrived after it would sit unadopted and unseen.

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
+refs/git-issues/v1/open/*:refs/git-issues/v1/open/*
+refs/git-issues/v1/closed/*:refs/git-issues/v1/closed/*
+refs/notes/git-issues/v1:refs/notes/git-issues/v1
```

Force is what makes the model work at all: two clones that both edited an issue
have divergent chains, and without force neither could push. With it, the last
writer wins — the loser's commits drop off the ref, and are recoverable from its
reflog: `git reflog show <the issue's ref>` lists every tip the ref has held,
and `git update-ref` puts one back.

That reflog exists because the store asks for it. `core.logAllRefUpdates=true`,
the default, logs `refs/heads/`, `refs/remotes/`, `refs/notes/` and `HEAD`, and
an issue ref is none of those, so there was no log at all and an overwritten tip
was a dangling commit until gc took it. Every write goes through `setRef`, which
passes `--create-reflog`, and git goes on logging a ref whose log exists — so one
local write covers every later overwrite, a fetch's included. The setting itself
is the user's, and covers refs that are not ours, so the store does not touch it.
The gap that remains is a ref this clone has only ever fetched and never written:
it has no log until the first local edit, and until then a forced fetch over it
leaves nothing behind.

Locally the store does better than this, which is what makes the transport's
behaviour a defect rather than a limit: every write is `git update-ref <ref> <new>
<old>`, a compare-and-swap, retried when the ref moved. Two processes editing one
issue in one clone cannot lose a write. `push` and `fetch` with a force refspec are
that compare-and-swap with the compare removed, across clones — the only place
it is hard to hold, and the only place it matters.

The same applies in reverse, which is the part that surprises people: `pull`
force-fetches, so a local issue that diverged from the remote is reset to the
remote's version. Comment locally, don't push, then pull, and the comment is off
the ref.

For the way this is used — one person editing an issue at a time, pushing when
they are done — that has been acceptable. It is still the single largest thing
wrong with the design, and everything else marked "read-only" or "one at a time"
in this document is downstream of it.

**A failed sync fails.** `Push` and `Fetch` attempt every refspec whatever the
others did, and answer what git refused, naming each refspec and its message, so
one unpushable issue neither abandons the rest nor passes for success. They used
to print a `Warning:` line and return nothing, and `pull` printed `Done.` and
exited 0 either way. What is not a failure stays quiet: a refspec matching nothing
locally, a ref the remote does not have, and a deletion of a ref that is already
gone are all "nothing to do", which is where every repository starts.

One thing still follows from the same code. **`push --all` deletes the counterpart
ref on the remote unconditionally:** closing an issue moves its ref, and the push
mirrors the move by deleting the remote's open ref — right when the
remote's open ref is an ancestor of the close, and a silent overwrite when someone
reopened the issue there and added to it.

The notes ref is the sharper edge: `refs/notes/issues` is one ref for the whole
repository, so force-pushing it replaces the remote's entire reverse index. A
link made in another clone and not yet fetched is dropped from the remote ref by
the next `push --all`. The issue itself still lists the commit — only the
commit → issue direction is lost, and `git issue link` again restores it.

Fixing this is designed and not yet built; see [Sync that does not lose a
write](#sync-that-does-not-lose-a-write). It would also unlock a writable `serve`.

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
  invokes it, and the transport is force-push. The section below is the design
  that replaces the assumption.

### Sync that does not lose a write

Tracked as `w4mr5qphh12kr9f2nxn0`; the plan, step by step, is
`docs/sketchy/issue-sync-plan.md`. As each step lands, its part of this section moves
into the body of this document.

**The tripwire.** The generation and adoption are built (see [gen0, and adopting
it](#gen0-and-adopting-it)); what is not is reading anything from a gen0 ref on a
*remote*. An old client cannot be refused — the remote is a plain git server with no
hook of ours — but once a remote has been pushed to by a client of this generation
it holds nothing in gen0, so a ref appearing there afterwards was pushed by an old
client, definitively. The work is kept and adopted, and the sync says so, to the
people who can pass on "please upgrade".

**Tracking refs.** Today a remote's issue refs are never held locally, so nothing can
say what the other side holds without asking the network, and nothing compares. `pull`
and `push` will first fetch the remote's refs, forced, into
`refs/git-issues/v1/remotes/<remote>/...` — a tracking ref is a copy and may be
overwritten — and decide locally, where ancestry is a local question.

**One chain, wherever its tips are held.** An issue is compared as the tips of its
chain — the local one, and the remote's in whichever namespace, old or new, open or
closed — and status follows the tip that wins. Each issue gets one verdict:

| verdict | `pull` | `push` |
|---|---|---|
| remote only | create the local ref | nothing |
| local only | nothing | make the remote right |
| equal | nothing | make the remote right |
| behind | fast-forward the local ref | nothing |
| ahead | nothing | make the remote right |
| diverged | refuse, unless `--force`; then merge | refuse, unless `--force`; then merge and push |

"Make the remote right" is one rule: the remote ends with exactly one ref for the
issue, the new-namespace ref in the local status, at the local tip, and every other
remote ref for it is deleted — safe exactly when the local tip descends from all of
them. It replaces the unconditional counterpart deletion, and it is how a remote is
migrated.

**Leases.** Every write to the remote is `git push --force-with-lease=<ref>:<what the
tracking ref says>`, non-force for a create or a fast-forward. That is the
compare-and-swap the transport lacked. A lease that fails means someone pushed since
the fetch: the issue is reported and left, and a re-run decides again.

**Merge.** A diverged issue is merged three-way: `git merge-tree` over the trees, where
`discussion/` unions by construction since its names cannot collide, and a text
conflict in `description.md` is refused as a person's to settle; `meta.tony` by value,
its list fields unioned. `status` goes to the side that changed it later, by the
committer date of the commit that last changed it since the merge base, a tie closing;
`closed_by` follows. Client version is not the rule: it says whether a write may be
trusted, and nothing about what its author meant, and two current clients disagreeing
is the ordinary case. The merge commit has both tips as parents, so both clones
fast-forward to it. Notes merge with `git notes merge -s union`, which git has always
had.

**Mixed versions.** A new client on a migrated remote cannot be hurt by an old one. An
old client there sees its own stale copy and no new issues, and is never told why; the
people told are the new clients' users, who can pass it on. Until a remote's first new
push, both share gen0 on it and the old hazards apply to the old client's pushes.

## Changes from the original design

| Original | Now | Why |
|---|---|---|
| `refs/issues/001`, counter ref | an issue ref keyed by XIDR | counters cannot be allocated offline |
| `labels.tony` | `labels` in `meta.tony` | one document, one write path |
| `discussion/<date>-<topic>.md`, hand-named | `discussion/<ts>-<hash>.md` | names that cannot collide on sync |
| `git issue discuss` | `comment`, `attach` | different inputs, different commands |
| `git issue label --remove` | `git issue unlabel` | reads better, parses simpler |
| Hook-driven `Closes #NNN` | explicit `close`, `link` | never built |
| "Web UI: non-goal" | read-only `serve` | a link to an issue is worth having |

## Known defects

- **No merge for issue refs, and a transport that overwrites.** The one described
  above, and the root of most of the rest. Designed, not built: [Sync that does not
  lose a write](#sync-that-does-not-lose-a-write).
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
