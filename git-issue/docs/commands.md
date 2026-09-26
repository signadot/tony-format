# Commands

Every command that takes an issue takes its id -- a 20-character XIDR such as
`j2dzt7xph12kswa9esn0` -- or any unambiguous prefix of one. A prefix matching
more than one issue is an error rather than a guess. Ids are minted locally
with no coordination, so two people filing issues offline cannot collide, and
an id is stable for the life of the issue, closing included.

Run `git issue -h` for the list, and `git issue <command> -h` for one command's
options.

## Create

```bash
git issue create "Issue title"                  # opens $EDITOR for the body
git issue create "Issue title" --body "text"    # body inline
echo "text" | git issue create "Issue title"    # body from stdin
```

The title becomes the first line of `description.md`. In the editor, lines
starting with `#` are stripped -- including markdown headings, so don't start a
line of real text with one. An empty body cancels.

## List

```bash
git issue list                    # open issues, newest first
git issue list --all              # closed ones and mirrors too
git issue list --label bug        # only issues carrying a label
```

## Show

```bash
git issue show j2dz
```

Prints the description, labels, linked commits and branches, related issues, the
discussion in chronological order, and the names of any attachments. A mirror
of another repository's issue says so, and where the issue lives.

## Edit the title or body

```bash
git issue edit j2dz --title "A better title"   # the title alone
git issue edit j2dz --body "The new body"      # the body alone
echo "The new body" | git issue edit j2dz      # the body, from stdin
git issue edit j2dz                            # opens $EDITOR on the description
```

The editor shows the description as stored -- `# Title` on the first line, the
body after it -- and takes back what you leave, headings included. An edit is a
commit on the issue's chain like any other, so history keeps what it said
before, and it races as any other write does: a comment or a pull that lands
meanwhile survives beside it, and two edits of the description at once leave
the later one.

## Comment

```bash
git issue comment j2dz "Comment text"     # inline
echo "text" | git issue comment j2dz      # from stdin
git issue comment j2dz                    # opens $EDITOR
```

With no text and no pipe, the editor opens in a temporary directory holding an
exported copy of the issue, so the existing discussion is there to read in
`./discussion/` while you write.

Comments are stored as `discussion/<timestamp>-<hash>.md`. The name is derived
from the content, so two clones adding different comments cannot land on the
same path; the timestamp sorts them.

## Attach files

```bash
git issue attach j2dz ./design.md          # single file
git issue attach j2dz ./test-results/      # directory tree
```

Attachments go under `discussion/files/`, keeping the layout they had.

## Labels

```bash
git issue label j2dz bug urgent
git issue unlabel j2dz urgent
git issue label j2dz severity=high    # a key and a value
git issue label j2dz severity=low     # replaces severity=high
git issue unlabel j2dz severity       # removes the key, whatever its value
```

Labels are lowercased and kept sorted, so `Bug` and `bug` are one label.

A label containing `=` is a key and a value, split at the first `=`, and a key
holds one value. Labels beginning `git-issue-` are reserved: git-issue, or a
program driving it (a phase machine, say, as `git-issue-phase=<phase>`), defines
what they mean.

When two clones' edits of one issue are merged, each side's additions and
removals are kept, and a key takes the value of whichever side changed it. A key
the two sides set to different values is a conflict: the pull names it and
leaves the issue alone, and it is settled by setting this clone's value to the
other's and pulling again, or by `pull --force`.

A merge made by an older git-issue keeps every label from both sides, so it can
leave a key two values. Reading such an issue takes the last value listed and
warns on stderr; `git issue label <id> key=<value>` puts it right.

## Link to a commit

```bash
git issue link j2dz abc123def     # by SHA
git issue link j2dz HEAD          # or anything git can resolve
```

The commit is recorded in full-SHA form on the issue, and the issue's id is
appended to the commit's note, which gives the reverse lookup:

```bash
git issue for-commit HEAD
git issue for-commit abc123
```

## Relate issues

```bash
git issue relate j2dz 4f1c          # general relationship
git issue blocks j2dz 4f1c          # j2dz blocks 4f1c
git issue duplicate j2dz 4f1c       # j2dz duplicates 4f1c
```

`blocks` is the one that writes both issues: the other end gets a matching
`blocked_by`, so the dependency reads the same from either side. `relate` and
`duplicate` record on the first issue only. When the other issue is a mirror of
another repository's (see [ext references](ext.md)), only this side is written.

## Close and reopen

```bash
git issue close j2dz                       # close
git issue close j2dz --commit abc123       # and record what closed it
git issue reopen j2dz
```

Closing moves the ref from `refs/git-issues/v1/open/<xidr>` to
`refs/git-issues/v1/closed/<xidr>`; the commit chain is untouched, so nothing is
lost and the id keeps resolving.

## Sync with a remote

```bash
git issue push                 # every issue to origin
git issue push j2dz            # one issue
git issue push --all upstream  # every issue, to another remote
git issue pull                 # fetch issues from origin
git issue pull --dry-run       # say what it would do, and write nothing
```

Both directions ask the remote what it holds, decide per issue, and then write.
An issue whose chain one side carries is sent or taken. An issue neither side's
chain carries is **merged**: comments union, and the later status change wins. So a
comment made here is not dropped by a pull, and one made elsewhere is not dropped
by a push.

What cannot be merged -- two people rewrote the same description -- is **left alone
and named**, and the command exits non-zero:

```
  j2dzt7xp  Fix the thing
      edited on both sides: description.md cannot be merged.
      here a1b2c3d4, origin e5f6a7b8.
      `git issue pull --force` takes the remote side; this clone stays in the ref's reflog.
```

`--force` is how you decide one: `pull --force` takes the remote's, `push --force`
takes this clone's, and what it overwrote stays in the ref's reflog either way.

Every write to a remote carries a lease on what the last fetch saw, so a push that
would land on top of someone else's is refused rather than forced. Closing an issue
moves its ref, and the push mirrors the move -- but only when this clone's tip carries
what the remote has, so a close cannot delete a reopen made elsewhere.

The reverse index merges by union in both directions, so a link made in another
clone survives. Mirrors and their sources go with the issues: push carries them to
this repository's origin, and pull brings each mirror up to its source (see
[ext references](ext.md)).

## Ext references

```bash
git issue ext add verse ~/src/verse          # a name for another repository
git issue ext fetch verse <full id>          # one of its issues, mirrored here
git issue ext refresh                        # bring every mirror up to its source
git issue ext remove <id>                    # drop a mirror, and the relations naming it
git issue ext list                           # the sources, and when each was fetched
```

See [ext references](ext.md) for what a mirror is and the rules it follows.

## Export and import

```bash
git issue export j2dz              # to ./j2dzt7xph12kswa9esn0/
git issue export j2dz ./my-issue
git issue import ./my-issue
```

Export writes the issue's whole tree plus a `.git-issue` breadcrumb recording
the ref and the commit it came from. Import replaces the issue's tree with the
directory's contents -- a file deleted in the directory is deleted in the issue --
and refuses to run if the issue moved since the export, unless given `--force`.

## Browse in a browser

```bash
git issue serve                      # http://localhost:8080/
git issue serve --addr 127.0.0.1:9000
```

Serves a read-only view of the issues in the current repository:

- `/` lists open issues; `/?all=1` includes closed ones
- `/i/<xidr>` is an issue. Prefixes work and redirect to the full-id URL,
  so the link you copy out of the address bar is the one that keeps resolving
- Links survive closing an issue: resolution searches the open and closed
  namespaces alike, so a URL pasted into chat does not rot when the ref moves
- Attachments download from `/i/<xidr>/files/<path>` as opaque bytes; nothing
  attached to an issue is ever rendered in the browser

`serve` is read-only: issues are edited with the CLI. There is no
authentication, and there should not be: bind loopback unless you know exactly
who else can reach the address you pick.

## Serve to an agent

```bash
git issue mcp                                  # this repository, and the configured set
git issue mcp -C ~/src/tony-format -C ~/src/verse
git issue mcp -poll 1s                         # look for changes made beside it every second
```

See [the MCP server](mcp.md).

## Migrations

Two one-shot upgrades for repositories that predate the current layout:

```bash
git issue migrate --dry-run           # numeric IDs -> XIDRs
git issue migrate

git issue migrate-comments            # discussion/NNN.md -> content-addressed
git issue migrate-comments --apply
```

`migrate-comments` is dry-run by default, backs each rewritten ref up to
`refs/issue-backup/<ts>/` unless given `--no-backup`, and is safe to re-run.

`migrate` is not. It re-identifies **every** issue it finds rather than only the
legacy ones, so a second run mints fresh ids for issues that already had them
and every id written down elsewhere stops resolving. Use `--dry-run` first, and
run it once.
