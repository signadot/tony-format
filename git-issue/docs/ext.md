# Ext references

A relation between issues is a bare id in `meta.tony`, and it means something
only where that id resolves. An issue in one repository that is about an issue
in another had no way to say so that a clone of the first repository alone could
follow. An ext reference is that way: the other repository's issue is mirrored
here, and from then on resolves here like any issue.

## Making one

```bash
git issue ext add verse ~/src/verse          # a name for the other repository, and where it is
git issue ext fetch verse <full id>          # its issue, mirrored under ext/verse/
git issue relate j2dz <that id>              # now resolves from this repository alone
git issue ext refresh                        # bring every mirror up to its source
git issue ext remove <that id>               # drop the mirror, and the relations naming it
git issue ext list                           # the sources, and when each was last fetched
```

A source is a name this repository gives another, as a remote is named, and
where it is. `ext fetch` names the issue by its full id: a prefix cannot be
resolved in a repository that is not here.

`relate`, `blocks` and `duplicate` to an issue of another repository of
[the set](commands.md#which-repository) do the `ext add` and `ext fetch`
themselves. The source is named for the other repository and is its
directory, unless this repository already has a source of that name, which
is then used as it is. `issue_relate` through the [MCP server](mcp.md) does
the same.

## The rules

**A mirror is read-only.** It is the source's chain, byte for byte, and only
the source's. A comment, an edit, a label, a close, a relation *from* it is
refused, naming the source and where it is. The far half of `blocks` is not
written onto a mirror: the relation is this repository's alone, which is what
this repository knows.

**It resolves like any issue.** `show`, `list --all` (marked with its source),
`for-commit` and a relation naming its id all find it. `list` without `--all`
does not list mirrors: they are not this repository's work. Its status is what
the source wrote in `meta.tony`, since the namespace here cannot say it.

**Refresh is a fetch that only moves forward.** A mirror follows the source's
chain and never merges with it, because nothing here adds to the chain. A source
that rewrote an issue's history is refused rather than followed.

**Sync carries mirrors.** A mirror is this repository's data -- a clone that
lacked it could not follow the relation -- so `push` sends every mirror and
source the remote lacks to this repository's origin, and never to a source.
`pull` takes what the remote holds and then brings each mirror up to its source;
a source that cannot be reached is named in the report and its mirrors stay as
they were. A source edited on both sides is merged: its URL is the side that
changed it since the two last agreed, and its fetch time the later one. Two
sides that changed the URL to different ones are refused and named, like an
issue edited on both sides, and so are two clones that recorded the source with
no history in common and disagree. `ext add` with the one to keep, then the
sync again, settles it, whichever side's it is: the record is made over the
version the sync refused, so the next sync carries it forward.

**A source's URL is every clone's.** The source travels with the mirrors, so
what it records is what every clone that pulls will fetch from. A directory on
this machine is not that: given one, as a path or a `file://` URL, `ext add`
records the directory's origin as the URL and keeps the directory in this
clone's git config, `git-issue.source.<source>.path`, as where this clone alone
fetches from. The directory stands for the URL, so once it is no longer a
checkout of it -- the record moved on, or the directory did -- a fetch is
refused, naming the config key, rather than following a repository the record
no longer names. A directory with no origin is recorded as it is, and `ext add`
and `ext list` say that only this machine can reach it. When a source cannot be
reached, the report says whether the URL it tried came from the shared record
or from this clone's config, so a wrong one is corrected where it is written.

**Removing one** deletes the mirror and, from every issue here that named it,
the relation -- as a commit on each such issue. Nothing is rewritten.

## Where they live

```
refs/git-issues/v1/ext/<source>/<xidr>     the source's chain for the issue
refs/git-issues/v1/sources/<source>        source.tony: the URL, and when last fetched
.git/config  git-issue.source.<source>.path   this clone's own path to it, if any; never carried
```

They are two namespaces because git cannot hold `ext/<source>` beside
`ext/<source>/<xidr>`: a ref is a file, and a file is not a directory. See
[storage](storage.md).

## What this keeps

The reference lives nowhere outside the repository. The distributed model --
every clone carries every ref, works offline, and syncs through git -- holds
for a relation across repositories exactly as it holds for one within. The
alternative, a pointer (`<id>@<repo>`) in `meta.tony`, would have answered an
id and a name offline and changed the metadata format; the mirror changes
none of it.
