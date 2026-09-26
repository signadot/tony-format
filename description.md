# git-issue: ext references -- another repository's issue mirrored here, read-only, so a relation across repositories resolves from this repository alone

## Why

A relation is a bare XIDR in meta.tony, and means something only where that id resolves. An
issue in verse that is about an issue in tony-format has no way to say so that a clone of
verse alone can follow: the id reads as "not found". So a relation across repositories is
refused (7qfhwth7h12ksxcwpxn0) until the format says what a cross-repository reference is.

This is what it is: a mirror. The other repository's issue is fetched into this one, as git
objects, under a namespace that says it is foreign and read-only. The reference is then
materialized in this repository -- a clone of it carries the mirrored issue, description and
discussion, and `show` follows the relation as it follows any other -- which is what keeps
the distributed model whole: nothing about the reference lives outside the repository.

The alternative, a pointer (`<xidr>@<repo>` in meta.tony), is cheaper and worse: offline it
answers an id and a name, no title, no status, no discussion, and it changes the metadata
format. The mirror changes none of it.

## The namespace

    refs/git-issues/v1/ext/<source>/<xidr>     the source's chain for the issue, byte for byte
    refs/git-issues/v1/ext/<source>            a sidecar: one commit holding source.tony, the
                                               source's URL, and nothing else

`<source>` is a name this repository gives the other, as a git remote is named; `<xidr>` is
the issue's id, unique across repositories, so it resolves here as it does there.

## The rules

1. **Read-only.** An ext issue is the source's chain and only the source's: a write to it --
   comment, edit, label, close, relate FROM it -- is refused, naming where it lives. The
   chain is never modified here, which is what makes a refresh a fast-forward and never a
   merge.
2. **It resolves like any issue.** `FindRef` searches `ext/` after `open/` and `closed/`;
   `show`, `list --all`, `for-commit` and a relation naming its id all find it. `list`
   without `--all` does not list ext issues: they are not this repository's work. A row that
   is one says its source.
3. **Refresh is pull's.** `git issue pull` fetches each ext issue from its source and
   fast-forwards the mirror; a source that cannot be reached leaves the mirror as it was and
   says so. The mirror is as fresh as its last refresh, and `show` says when that was (the
   fetch's time, in the sidecar).
4. **Push carries ext refs to THIS repository's origin**, because they are this repository's
   data -- a snapshot it depends on -- so other clones of it get the mirror without knowing
   the source. They are never pushed to the source. The lease and the verdicts apply to them
   as to any ref; an ext ref that diverged (two clones refreshed to different points of the
   source's chain) takes the one that carries the other, and both do, since they are the
   same chain.
5. **Making one.** `git issue ext add <source> <url>` records the sidecar; `git issue ext
   fetch <source> <xidr>` mirrors an issue; `git issue relate <id> <ext-id>` then works as
   it does. The MCP server's `issue_relate` (7qfhwth7h12ksxcwpxn0), given two ids in
   different repositories it serves, does the mirroring itself: it has both stores, so the
   "source" is the other repository's path and the fetch is local.
6. **Removing one** removes the mirror and the relations naming it, in that order, as a
   commit on each issue that named it; nothing is rewritten.

## What it takes

1. `ext/` in namespace.go and FindRef; `IsExtRef`, `ExtSource(ref)`; the sidecar's read and
   write. Tracking refs for `ext/` under `remotes/<remote>/ext/`, as open and closed have.
2. The read-only refusal in ops: every write resolves its id and refuses an ext ref.
3. `ext add`, `ext fetch`, `ext remove`; pull's refresh; push's carry.
4. Tests: mirror an issue from a second scratch repository; relate to it; a clone of the first
   repository alone shows the relation whole; a write to the mirror refused; a refresh after
   the source moved fast-forwards; push carries it to origin and never to the source.
5. README: the namespace and the rules, in the storage model section.

## Not this

- A mirror of a whole repository's issues. One issue at a time, because a reference is to
  one issue; a repository that wants another's tracker whole clones it.
- A write-through to the source. The comment goes where the issue lives.