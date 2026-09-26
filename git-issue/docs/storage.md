# Storage

## Refs

- `refs/git-issues/v1/open/<xidr>` -- an open issue
- `refs/git-issues/v1/closed/<xidr>` -- a closed issue
- `refs/git-issues/v1/ext/<source>/<xidr>` -- another repository's issue, mirrored here
- `refs/git-issues/v1/sources/<source>` -- where that repository is (`source.tony`)
- `refs/notes/git-issues/v1` -- reverse index, commit -> issue ids

Status is the namespace: an issue is open because its ref is under the open
namespace. `meta.tony` carries a `status` field too, but where the ref lives
is what listings believe. A mirror is the exception: its status is what the
source wrote in `meta.tony`, since the namespace here cannot say it.

The `v1` is a generation. A client names its refspecs literally, so it cannot
see or touch a ref it does not name; a client older than the generation syncs
`refs/issues/` and `refs/closed/` and is harmless to anything written here.
The generation changes only for a break a client of the previous one cannot
safely coexist with. What an older client left in a clone is adopted on the
next read.

## The issue tree

Each ref points at a commit chain whose tree holds the issue:

```
description.md                         # title (first line) and body
meta.tony                              # metadata
discussion/
  20260807T211114Z-4ca263f7.md         # a comment
  20260808T093012Z-1b0e55a3.md         # another
  files/
    design.md                          # attached file
    test-results/                      # attached directory
      output.log
      metrics.json
```

Every operation appends a commit, so an issue's history is git history:

```
$ git log --oneline refs/git-issues/v1/open/j2dzt7xph12kswa9esn0
a996527 comment: a comment
63cc372 link: c477908
768c0fb label: added bug, urgent
e17a84d create: issue j2dzt7xph12kswa9esn0
```

Writes go through git's plumbing over a temporary index, so filing an issue
never disturbs your index or working tree.

## meta.tony

```tony
!issue
blocked_by: []
blocks: []
branches: []
commits:
- c477908970ca0fe72f2355754f778a24fdd6bdfd
created: "2026-08-07T23:11:13.920433+02:00"
duplicates: []
id: j2dzt7xph12kswa9esn0
labels:
- bug
- urgent
related_issues: []
status: open
updated: "2026-08-07T23:11:14.239981+02:00"
```

`closed_by` appears once something closes the issue. The document is generated
from the `Issue` struct in `issuelib`, so adding a field is a matter of adding it
there and regenerating (`make generate`).

## Identifiers

An issue is named by its XIDR, a 20-character base32 string assigned at
creation:

```
j2dzt7xph12kswa9esn0
```

An XID is 12 bytes: timestamp, machine, process, counter. An XIDR is the same
bytes reversed. Reversal is the whole trick: it puts the counter and machine
first, so the three or four characters someone actually types are already the
part that varies. Unreversed, every issue filed in the same second would share
its opening characters and no short prefix would resolve.

Ids are minted locally with no coordination -- two people filing issues offline
cannot collide -- and are stable for the life of the issue, including across
closing it. Issues created before XIDRs used six-digit sequential numbers; those
are still readable, and `git issue migrate` converts them.

## Comment names

Comments are `discussion/<timestamp>-<hash>.md`: a sortable UTC timestamp and a
short digest of the content. The name is derived from the content, so two
clones adding different comments never land on the same path, and an identical
re-add collapses to one. The timestamp keeps names sorting chronologically;
`show` reads the timestamp inside each comment rather than trusting the name.

## The reverse index

`refs/notes/git-issues/v1` holds, for a commit, the ids of the issues that link
it, one per line. Appending is idempotent, and the index merges by union in
both directions of a sync, so a link made in another clone survives.
