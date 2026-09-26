# The distributed model

git-issue has one idea: an issue is part of the repository, and so is treated
the way git treats everything in a repository. There is no server, no database
and no account. What follows is that idea, and what it gives you.

## An issue is a ref

Each issue is a ref, `refs/git-issues/v1/open/<id>`, pointing at a commit
chain whose tree is the issue: a description, metadata, the discussion, any
attached files. It is in the repository but not in the working tree, so it never
appears in a diff, a merge or a `git status`, and checking out an old commit
does not take the issues back in time with it.

## Every clone is complete

A clone that has pulled the issues holds all of them, and every operation
works on the clone alone: create, comment, label, relate, close. Nothing is
reached. You work on the train, and the tracker works with you. `git clone`
fetches branches and tags, not issue refs, so a fresh clone's first step is
`git issue pull`.

## Ids never collide

An id is minted where the issue is created, from the time, the machine, the
process and a counter, and reversed so the characters that vary come first and
a three-character prefix names it. Two clones filing issues at the same moment
file two issues. Nothing is allocated, so nothing needs to be asked.

## Sync is push and pull

`git issue push` and `git issue pull` move issues the way `git push` and
`git pull` move branches, and decide the same way: per issue, from what each
side holds.

- An issue only one side has is sent or taken.
- An issue one side has moved further is brought forward on the other.
- An issue both sides changed is **merged**: the discussion unions, labels
  keep both sides' changes, and the later status change wins.
- What cannot be merged, such as a description rewritten on both sides, is
  **named and left alone**, and `--force` is how a person decides it. What a
  force overwrote stays in the ref's reflog.

A push carries a lease on what the last fetch saw, so it cannot land on top of
a change it has not seen. Nothing is lost quietly.

## History is git history

Every change is a commit appended to the issue's chain, named for what it did:
`create`, `comment`, `label: added bug`, `close`. `git log` on the ref is the
issue's history, with no second log to keep consistent. Closing moves the ref
to `refs/git-issues/v1/closed/<id>`; the chain is untouched, and the id keeps
resolving.

## Across repositories

An issue in one repository that is about an issue in another mirrors that
issue here, as an [ext reference](ext.md), under a read-only ref. The
relation then resolves from this repository alone, and the mirror syncs with
this repository's other refs. The model holds across repositories exactly as
it holds within one: nothing lives outside a repository.

## What it costs

Issues are a second thing to push, and a fresh clone must pull them. A merge
that cannot be made is a person's to settle, as it is for code. And the refs
are visible to anyone who can read the repository, so an issue is as public as
the code beside it.

For the reasoning behind each choice, see [the design](design.md).
