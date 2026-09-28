# Commands

Every command that takes an issue takes its id -- a 20-character XIDR such as
`j2dzt7xph12kswa9esn0` -- or any unambiguous prefix of one. A prefix matching
more than one issue is an error rather than a guess. Ids are minted locally
with no coordination, so two people filing issues offline cannot collide, and
an id is stable for the life of the issue, closing included.

Run `git issue -h` for the list, and `git issue <command> -h` for one command's
options.

## Which repository

A command runs on one repository. Inside a repository that is the one, as
git has it. `~/.config/git-issue.tony` (`$XDG_CONFIG_HOME/git-issue.tony` when
set) names the repositories you work in, and with the one a command is run in
they are the set:

```tony
repos:
- /Users/you/src/tony-format
- /Users/you/src/verse
```

Full paths: nothing expands `~`. A repository is named in the set by its
directory's name, `verse`, or by its path when two have the same.

```bash
git issue show j2dz                       # the repository that holds j2dz
git issue create --repo verse "A title"   # verse, from anywhere
git issue list                            # outside a repository: every one
```

- `--repo <name>` names the repository: a name of the set's, or any
  repository's directory.
- A command given an issue runs on the repository that holds it: the one it
  is run in, when that holds it, and otherwise the one of the set that does.
  An id is unique across repositories. The command says which it ran on.
- A command that makes one thing in one place -- `create`, `import`,
  `for-commit`, `ext add`, `ext fetch`, `serve` -- runs on the repository it
  is run in. Outside one it needs `--repo`, unless the set is one repository.
- A command that reads or syncs, given no issue -- `list`, `watch`, `pull`,
  `push`, `ext list`, `ext refresh` -- covers every repository of the set
  when it is run outside one. `list` and `watch` say which on each line; the
  rest run on each repository in turn, under its name, and one that fails
  does not stop the others. `watch` given issues watches the repositories
  that hold them.
- `relate`, `blocks` and `duplicate` to an issue of another repository of the
  set mirror it into this one first ([ext](ext.md)).
- A commit a command is given -- `link`, `close --commit`, `for-commit` -- is
  one of the repository the command runs on.
- `migrate` and `migrate-comments` run on the repository they are run in.

Outside a repository with no set, a command says there is no repository.

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
what the remote has, so a close cannot delete a reopen made elsewhere. A push
that lands brings this clone's copy of the remote to what it wrote, as the
next fetch would.

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
git issue serve -watch               # pull origin; open pages reload as issues change
```

Serves a read-only view of the issues in the current repository, and prints
the URL to open. `localhost` is listened on at both its addresses, 127.0.0.1
and ::1, so the name reaches `serve` however a browser resolves it. If
something already answers at an address, `serve` refuses and names it: give
`--addr` another port.

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

Without `-watch` a page says what this clone holds when it is loaded, and
nothing is pulled. With it, `serve` runs the watch [`watch`](#watch) runs,
and takes its `--remote`, `--local`, `-poll` and `-fetch`:

- it pulls origin every `-fetch`, when there is an origin
- an open issue's page reloads when the issue changes, and the list when any
  does
- an issue refused, or a remote not reached, is shown at the top of the list
  and of that issue's page until it clears

The pull writes this clone's refs, as `git issue pull` does. The view is still
read-only: nothing a browser sends changes an issue. A page reloads by a
script, `/watch.js`, the only one served, which asks the server every two
seconds what changed.

## Serve to an agent

```bash
git issue mcp                                  # this repository, and the configured set
git issue mcp -C ~/src/tony-format -C ~/src/verse
git issue mcp -poll 1s                         # look for changes made beside it every second
git issue mcp -fetch 10s                       # issue_watch_remote pulls every 10 seconds
```

See [the MCP server](mcp.md).

## Watch

```bash
git issue watch                  # every issue here, pulling origin
git issue watch j2dz 4f1c        # these two
git issue watch --label bug      # those labeled bug, before or after
git issue watch --remote up      # pull up, not origin
git issue watch --local          # this clone only; pull nothing
git issue watch -poll 1s         # look every second, not every 5
git issue watch -fetch 10s       # pull every 10 seconds, not every 30
```

`watch` prints a line for each issue that changes, as it changes, until
stopped:

```
pull origin: j2dzt7xph12kswa9esn0  1c4e2a0..9b7d3f1
j2dzt7xph12kswa9esn0  open (on origin)  Implement streaming processor  -- comment: looks good; label: added bug
j2dzt7xph12kswa9esn0  closed (local)  Implement streaming processor  -- close
j2dzt7xph12kswa9esn0  closed (on origin)  Implement streaming processor  -- pushed to origin
```

A change line is the id, the status, where the change is, the title, and what
was done: one entry per commit the issue gained, then `closed` or `reopened`
if it moved with no commit saying so. An issue new to the watch lists only
commits made since `watch` started, or `arrived` when there are none.

Where the change is says whether the remote the watch pulls (origin, with
`--local`) holds the issue as the change left it: `on origin`, or `local`
when it does not, and the change is not pushed. It is read from this clone's
copy of the remote, which a pull's fetch and a push bring up to date. A
change a pull brought is on origin; one a pull merged with a change made
here is local until pushed. When a push brings origin a change that was
local, `watch` says `pushed to origin`. With no origin nothing is said.

Issues often travel by push and pull, so `watch` pulls origin every `-fetch`
(30s by default) when there is an origin. What a pull did to an issue is said
just before the change it brought, as `pull` says it (`created at <sha>`,
`<old>..<new>`, `merged <a> and <b>`). An issue among those watched refused,
or a remote not reached, stands until a person acts. It is said when it begins, not on every
pull, and `clear` is said when nothing stands. A remote that does not answer
within two minutes is given up on until the next pull, and said as `could not
pull`. `watch` pulls once when it starts, and what that brings is where it
begins. It pushes nothing.

Between pulls it compares this clone's refs every `-poll` (5s by default), so
a change made here is heard within that, whoever makes it; one made during a
pull is heard when the pull is done.

`watch` is for an agent whose host wakes it on a background command's output,
as Claude Code's Monitor does. Such an agent hears its own work: a line for
each change it makes, and a `pushed to origin` line for each issue its push
sends. Those lines confirm what it did. They are not news to act on. An agent that can wait on a tool call uses
[`issue_watch` or `issue_watch_remote`](mcp.md#the-watch).

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
