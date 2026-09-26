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

A source is a name this repository gives another, as a remote is named, and a
URL or a path. `ext fetch` names the issue by its full id: a prefix cannot be
resolved in a repository that is not here.

Through the [MCP server](mcp.md), `issue_relate` across two served repositories
does the `ext add` and `ext fetch` itself, the other repository's directory
being the source.

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
they were.

**Removing one** deletes the mirror and, from every issue here that named it,
the relation -- as a commit on each such issue. Nothing is rewritten.

## Where they live

```
refs/git-issues/v1/ext/<source>/<xidr>     the source's chain for the issue
refs/git-issues/v1/sources/<source>        source.tony: the URL, and when last fetched
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
