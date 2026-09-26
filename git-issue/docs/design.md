# git-issue design

This is the design of the tracker as built, and the reasoning behind the parts
that are not obvious. The idea itself is one page, [the distributed
model](model.md); for how to use it, see the [README](../README.md) and
[commands](commands.md); for the API, see the package docs in `issuelib`, `ops`
and `commands`.

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
- A writable web UI. `git issue serve` is a read-only viewer, and the agent
  interface is MCP over stdio, not HTTP.

## Storage model

An issue is a git ref pointing at a commit chain. The ref namespace carries the
status, under a generation that says which ref layout this is:

```
refs/git-issues/v1/open/<xidr>            an open issue
refs/git-issues/v1/closed/<xidr>          a closed issue
refs/git-issues/v1/ext/<source>/<xidr>    another repository's issue, mirrored here
refs/git-issues/v1/sources/<source>       where that repository is
refs/notes/git-issues/v1                  reverse index, commit -> issue IDs
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

Metadata is in Tony rather than JSON or YAML because it reads and diffs like
text, has real types rather than strings for everything, carries comments, and
is this project's own format: dogfooding it here is the point.

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
different content, and one ref name for both. No sync can do anything sensible
with that.

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

### Labels with a key, merged against the base

A program driving git-issue -- verse's phase machine is the first -- needs a
value per issue that every client carries and a race settles. Labels are the one
field every client already keeps through an edit; a new meta.tony field is
dropped by the first older client to write the issue, and a ref per phase needs
its own sync. So a label containing `=` is a key and a value, a key holds one
value, and `git-issue-` is reserved for such conventions.

What stood in the way was the merge. It took the union of the two sides'
labels, and of every other list, so a removal on one side was undone by any
edit on the other: an `unlabel` lost to a comment, and a phase moved on one side
coming back as two phases. The merge is now three-way. Each list keeps both
sides' additions and removals; a key takes the answer of whichever side changed
it; and a key two sides changed to different answers is a conflict, refused like
a description rewritten on both sides, since two people moving one key at once
is theirs to settle.

An older client's merge is still a union, and can leave a key two values. That
is read, not refused: the last value listed is taken, a warning goes to stderr,
and `git issue label <id> key=<value>` puts it right. Nothing tries to decide
which value was meant, because nothing in the labels says.

### Ext references, not pointers

A relation is a bare id in `meta.tony`, meaningful only where the id resolves,
so an issue about another repository's issue had no way to say so that a clone
could follow. Two designs answered it. A pointer, `<id>@<repo>`, in `meta.tony`
is cheap to write and answers an id and a name offline, but nothing more: the
title, the status, the discussion are all a network away, and every reader of
the field has to learn the syntax. The other is to mirror the issue: fetch the
other repository's chain for it under `refs/git-issues/v1/ext/<source>/<xidr>`,
and let the existing id resolution find it there.

The mirror won because it changes nothing. The relation is a bare id as before;
`show`, `for-commit` and the sync carry the mirror as they carry any ref; a
clone that pulled has what it needs to follow the relation. What the mirror
must not do is diverge, so it is read-only here, refreshed only forward, and
never written to its source: an id is unique across repositories, so an id
names its repository, and that is where it is edited.

The source's URL and last fetch time live in a sidecar ref,
`refs/git-issues/v1/sources/<source>`, and not at `ext/<source>`, because a ref
is a file and git cannot hold a file beside a directory of the same name.

### One store, many front ends

The CLI and the MCP server are two front ends over one `ops` package: one
function per operation, over the `Store`. Each front end parses what it was
given and calls the same function, so what "close" means -- the commit
verified, the message written, the ref moved -- is decided once, and an
operation answers what it did as data for the front end to show. `GitStore`
takes a directory rather than the process's working directory, which is what
lets one server hold several repositories and dispatch an id to the one that
owns it.

### Plumbing over a temporary index

Writes go through `hash-object`, `mktree`/`update-index`, `commit-tree` and
`update-ref`, with `GIT_INDEX_FILE` pointed at a temporary file. Nothing touches
the caller's index or working tree, so filing an issue in the middle of a messy
edit is safe, and no operation needs a clean tree to run.

### Read-only `serve`

`serve` exists because nothing else in the tool can hand someone a link. It reads;
it does not write. That was once a consequence of the sync model; it is now
simply a line not yet crossed, since [Sync](#sync-and-what-it-costs) merges what
two writers leave. Markdown is rendered with goldmark's default (safe) renderer
rather than a hand-rolled subset, since issue text is attacker-controlled in the
sense that matters -- served from the same origin as everything else -- and
escaping is the part of a renderer that is easiest to get subtly wrong: raw HTML
becomes a placeholder comment and `javascript:` link targets are refused.

There is no authentication and should not be; the default bind is loopback.

## Sync, and what it costs

A remote's refs are fetched into this clone before anything is decided, under

```
refs/git-issues/v1/remotes/<remote>/open/<xidr>
refs/git-issues/v1/remotes/<remote>/closed/<xidr>
refs/git-issues/v1/remotes/<remote>/gen0-open/<xidr>
refs/git-issues/v1/remotes/<remote>/gen0-closed/<xidr>
refs/git-issues/v1/remotes/<remote>/ext/<source>/<xidr>
refs/git-issues/v1/remotes/<remote>/sources/<source>
refs/notes/git-issues/remotes/<remote>/v1
```

as `refs/remotes/` is for branches. That is what makes ahead, behind and diverged
local questions, which git answers with `merge-base`. They are fetched forced and
pruned, because a tracking ref is a copy of what the remote has and not a history
of its own.

An issue is **one chain**, whichever namespace each side keeps it in, so the
comparison is of commits and the status follows the tip that wins. The remote's
tip is whichever of its refs for the issue carries all the others, a closed one
breaking a tie; no such tip means the remote's own refs disagree, which only a
client of another generation pushing after this one can cause.

| verdict | `pull` | `push` |
|---|---|---|
| remote only | create it here | nothing |
| local only | nothing | make the remote right |
| equal | take the status if it differs | make the remote right |
| behind | bring this clone forward | nothing |
| ahead | nothing | make the remote right |
| diverged | merge, or refuse what cannot be merged | merge and send, or refuse |

**"Make the remote right"** is one rule: the remote ends holding exactly one ref
for the issue, this generation's, in the status this clone has it in, at this
clone's tip, and every other ref it had for the issue is deleted. That one rule
sends an issue, mirrors a close, and migrates an issue the remote only ever had
in gen0 — which is why there is no migration command. It is safe precisely when
this clone's tip carries every tip the remote has, which is what the verdict says.

**Diverged is the one verdict that can lose work**, and the only one that is not
a matter of moving a ref. It is merged, below; where it cannot be, it is refused.
What a `--force` overwrites stays in the ref's reflog: `git reflog show <the
issue's ref>` lists every tip the ref has held, and `git update-ref` puts one back.

**A divergence is merged, not chosen between.** The two chains share a root, and
in the ordinary case nothing about them conflicts: `discussion/` unions by
construction, since its names are `<timestamp>-<hash>` and cannot collide, and
`meta.tony`'s lists are sets. The trees are merged by `git merge-tree`, and
`meta.tony` is then replaced by one merged **by value** — a text merge of a
generated file is how two orderings of one list become a conflict about nothing.
The result has both tips as parents, so whoever syncs next fast-forwards to it
and neither clone is told it lost.

`status` is the one field with two defensible answers, and **the later change
wins**, by the committer date of the commit that last made one on each side.
Whoever acted second acted knowing more: a reopen after a close means someone
looked again, and closing it back would be this tool overruling them. A side that
changed nothing does not compete, neither side changing anything leaves the
base's, and a tie closes. `closed_by` follows the decision.

Which client wrote a change is not the rule. A version says whether a write can be
trusted — which is what the generation above is for — and nothing about what its
author meant, and two clients of one version disagreeing is the ordinary case.

**What cannot be merged is refused.** A conflict in `description.md`, or any path
but `meta.tony`, means two people rewrote the same text, and no rule here beats
asking them: the issue is named with the path, the run exits non-zero, and
`--force` is how one of them decides it — taking one side outright, with the
other left in the ref's reflog.

**Every write to a remote carries a lease.** `--force-with-lease=<ref>:<what the
tracking ref said>` refuses the write if the remote moved since the fetch, and an
issue's refs go in one `--atomic` push so they change together or not at all.
Issues share that push, a couple of hundred to one, because a push is a connection
and a connection per issue made syncing a repository take minutes; the issues the
remote refuses are named, and the rest go again without them. That
is the compare-and-swap `setRef` has always made locally, at last reaching the
wire — the property the transport lacked, in the only place it is hard to hold and
the only place it matters.

The reflog exists because the store asks for it. `core.logAllRefUpdates=true`,
the default, logs `refs/heads/`, `refs/remotes/`, `refs/notes/` and `HEAD`, and
an issue ref is none of those, so there was no log at all and an overwritten tip
was a dangling commit until gc took it. Every write goes through `setRef`, which
passes `--create-reflog`, and git goes on logging a ref whose log exists — so one
local write covers every later overwrite, a fetch's included. The setting itself
is the user's, and covers refs that are not ours, so the store does not touch it.
The gap that remains is a ref this clone has only ever fetched and never written:
it has no log until the first local edit, and until then a forced fetch over it
leaves nothing behind.

**A remote is migrated by being pushed to**, and the gen0 namespace on it becomes
a tripwire. Once a client of this generation has pushed, the remote holds nothing
there — so a gen0 ref appearing afterwards was pushed by a client too old to see
the current refs. Its work is kept, adopted like any other tip, and the sync says
so. An old client cannot be *refused*: the remote is a plain git server with no
hook of ours. But the people told are the ones who can pass on "please upgrade".

**Mixed versions**, then: a client of this generation cannot be hurt by an older one
on a migrated remote, since the older one cannot name a ref of this generation. The
older one there sees its own stale copy and no new issues, and is never told why — a
fetch matching nothing deletes nothing. Until a remote's first push from this
generation, both share gen0 on it and the older hazards apply to the older client's
pushes.

**A failed sync fails.** What git refused is answered rather than warned about,
naming each refspec and its message, and a sync that refused or failed anything
exits non-zero. What is not a failure stays quiet: a refspec matching nothing
locally, a ref the remote does not have, and a deletion of a ref that is already
gone are all "nothing to do", which is where every repository starts.

**Planning is separate from writing**, and is a pure function of refs and
ancestry. That is what `--dry-run` prints, and what the tests drive by putting
refs where a fetch would have.

**The reverse index** merges rather than replaces. It is one ref for the whole
repository, so force-pushing it used to replace whatever the remote had: a link
made in another clone and not yet fetched was dropped by the next `push --all`,
and only `git issue link` again restored it. Both directions now merge it with
`git notes merge -s union`, which is what a reverse index wants and what git has
always had, so two clones that linked different commits keep both links. A gen0
index is folded in the same way and then cleared from the remote.

A pull no longer leaves an issue in both namespaces, since it decides which one
each issue is in before writing. `CleanupStaleRefs` stays for a repository that
already held such a pair, and keeps its old rule: the ref with more history, or
the closed one when neither descends from the other.

## Not built

From the original design, still absent:

- **Commit-message integration.** `Closes #NNN` / `See #NNN` parsed by a
  `post-commit` hook. Nothing installs a hook and nothing parses commit messages;
  linking and closing are explicit commands. The XIDR makes the syntax less
  attractive than it looked with short numeric IDs. The convention that stands
  in for it is a trailer, `Issue: <id>`, on the commit, and `link`.
- **Branch linking.** `meta.tony` has a `branches` field and `show` prints it,
  but no command writes it.
- **`git issue discuss`.** Split into `comment` (text) and `attach` (files), which
  are different enough operations to want different arguments.

And never designed, by choice or so far: a writable `serve`; milestones,
sprints and assignment (authorship is the commit's); full-text search, which
`git log` and `git grep` already reach; `git issue cat` for one attachment
without exporting the issue; notifications; a bridge to another tracker.

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

## Known defects and limits

- **`git issue migrate` is not idempotent.** It re-identifies every issue it
  finds rather than only the legacy-numeric ones, so a second run mints fresh
  XIDRs for issues that already had them and every ID recorded elsewhere stops
  resolving. Filtering on `IsLegacyRef` would fix it.
- **A description rewritten on both sides is refused, not merged**, and
  `--force` picks a side. That is the design; it is listed here because it is
  the one place a sync stops and waits for a person.
- **A fresh clone has no issues** until `git issue pull`, since `git clone`
  fetches branches and tags only.
- **Issues are as visible as the repository.** Anyone who can read the refs can
  read the issues; there is no finer grain.

## Status

Implemented and in use. `refs/meta/issue-counter` no longer allocates anything;
`push` and `pull` still carry its refspec, harmlessly, for repositories that
still have the ref.
