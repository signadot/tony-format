# git-issue

A git-native issue tracker. Issues live in the repository they describe, as git
refs, so they clone, branch, work offline and sync with the code rather than
beside it.

## Features

- **Git-native storage**: issues are refs (`refs/issues/*`, `refs/closed/*`) over
  ordinary git objects — no database, no server, no sidecar files in the tree
- **Collision-free IDs**: every issue gets an XIDR, unique across clones without
  anyone allocating it, and you type as much of it as it takes to be unambiguous
- **Rich discussions**: comments, file attachments and directory trees
- **Commit linking**: issue → commit, and commit → issue via git notes
- **Labels**: add, remove, filter listings
- **Tony format**: metadata is human-readable Tony, content is markdown
- **Shareable links**: `git issue serve` puts a stable URL on every issue

## Installation

```bash
cd /path/to/go-tony
go build ./cmd/git-issue
mv git-issue /usr/local/bin/     # anywhere on PATH
```

The binary must keep the name `git-issue`: that is how `git issue ...` reaches
it. Run it directly as `git-issue ...` if you would rather not install it.

## Identifiers

An issue is named by its XIDR, a 20-character base32 string assigned at
creation:

```
j2dzt7xph12kswa9esn0
```

Every command that takes an ID also takes a prefix of one, so in practice you
type three or four characters:

```bash
git issue show j2dz
```

A prefix matching more than one issue is an error rather than a guess. IDs are
minted locally with no coordination — two people filing issues offline cannot
collide — and are stable for the life of the issue, including across closing it.

Issues created before XIDRs used six-digit sequential numbers. Those are still
readable, and `git issue migrate` converts them.

## Usage

### Create an issue

```bash
git issue create "Issue title"                  # opens $EDITOR for the body
git issue create "Issue title" --body "text"    # body inline
echo "text" | git issue create "Issue title"    # body from stdin
```

The title becomes the first line of `description.md`. In the editor, lines
starting with `#` are stripped — including markdown headings, so don't start a
line of real text with one. An empty body cancels.

### List issues

```bash
git issue list                    # open issues, newest first
git issue list --all              # including closed
git issue list --label bug        # only issues carrying a label
```

### Show issue details

```bash
git issue show j2dz
```

Prints the description, labels, linked commits and branches, related issues, the
discussion in chronological order, and the names of any attachments.

### Link an issue to a commit

```bash
git issue link j2dz abc123def     # by SHA
git issue link j2dz HEAD          # or anything git can resolve
```

The commit is recorded in full-SHA form on the issue, and the issue's ID is
appended to the commit's note under `refs/notes/issues`, which gives the reverse
lookup:

```bash
git issue for-commit HEAD
git issue for-commit abc123
```

### Add comments

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

### Attach files

```bash
git issue attach j2dz ./design.md          # single file
git issue attach j2dz ./test-results/      # directory tree
```

Attachments go under `discussion/files/`, keeping the layout they had.

### Labels

```bash
git issue label j2dz bug urgent
git issue unlabel j2dz urgent
```

Labels are lowercased and kept sorted, so `Bug` and `bug` are one label.

### Link issues together

```bash
git issue relate j2dz 4f1c          # general relationship
git issue blocks j2dz 4f1c          # j2dz blocks 4f1c
git issue duplicate j2dz 4f1c       # j2dz duplicates 4f1c
```

`blocks` is the one that writes both issues: the other end gets a matching
`blocked_by`, so the dependency reads the same from either side. `relate` and
`duplicate` record on the first issue only.

### Close and reopen

```bash
git issue close j2dz                       # close
git issue close j2dz --commit abc123       # and record what closed it
git issue reopen j2dz
```

Closing moves the ref from `refs/issues/<xidr>` to `refs/closed/<xidr>`; the
commit chain is untouched, so nothing is lost and the ID keeps resolving.

### Sync with a remote

```bash
git issue push j2dz            # one issue to origin
git issue push --all           # every issue
git issue push --all upstream  # to another remote
git issue pull                 # fetch issues from origin
```

Both directions force refspecs and nothing merges issue refs, so the last writer
of a given issue wins. That cuts both ways: `pull` resets a local issue that
diverged from the remote, so push your edits before pulling. In practice this is
fine — issues are edited by one person at a time — but it is the reason `serve`
is read-only and the reason comments are stored under content-addressed names.

`pull` also cleans up an issue that arrived in both namespaces, keeping whichever
ref has more history.

### Export and import

```bash
git issue export j2dz              # to ./j2dzt7xph12kswa9esn0/
git issue export j2dz ./my-issue
git issue import ./my-issue
```

Export writes the issue's whole tree plus a `.git-issue` breadcrumb recording
the ref and the commit it came from. Import replaces the issue's tree with the
directory's contents — a file deleted in the directory is deleted in the issue —
and refuses to run if the issue moved since the export, unless given `--force`.

### Browse issues in a browser

```bash
git issue serve                      # http://localhost:8080/
git issue serve --addr 127.0.0.1:9000
```

Serves a read-only view of the issues in the current repository:

- `/` lists open issues; `/?all=1` includes closed ones
- `/i/<xidr>` is an issue. XID prefixes work and redirect to the full-XIDR URL,
  so the link you copy out of the address bar is the one that keeps resolving
- Links survive closing an issue: resolution searches both `refs/issues/` and
  `refs/closed/`, so a URL pasted into chat does not rot when the ref moves
- Attachments download from `/i/<xidr>/files/<path>` as opaque bytes; nothing
  attached to an issue is ever rendered in the browser

`serve` is read-only by design, not as a first cut. Issue sync is a force-push
in both directions with no merge step, so a second writer would silently drop
whichever update lost the race. Issues are edited with the CLI.

There is no authentication, and there should not be: bind loopback unless you
know exactly who else can reach the address you pick.

### Migrations

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
legacy ones, so a second run mints fresh XIDRs for issues that already had them
and every ID written down elsewhere stops resolving. Use `--dry-run` first, and
run it once.

## Storage Model

### Git refs

- **`refs/issues/<xidr>`** — an open issue
- **`refs/closed/<xidr>`** — a closed issue
- **`refs/notes/issues`** — reverse index, commit → issue IDs

Status is the namespace: an issue is open because its ref is under
`refs/issues/`. `meta.tony` carries a `status` field too, but where the ref lives
is what listings believe.

### Issue structure

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
$ git log --oneline refs/issues/j2dzt7xph12kswa9esn0
a996527 comment: a comment
63cc372 link: c477908
768c0fb label: added bug, urgent
e17a84d create: issue j2dzt7xph12kswa9esn0
```

### Metadata format (meta.tony)

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
there and regenerating.

## Design Principles

1. **Git-native**: git's object storage, refs and notes, driven through plumbing
   commands over a temporary index, so writing an issue never disturbs your index
   or working tree
2. **Distributed**: works offline, syncs via git push/pull, needs no server
3. **Human-readable**: Tony for metadata, markdown for content
4. **Auditable**: full history via `git log` on an issue's ref
5. **Extensible**: new fields are a struct field and a regeneration away

## Examples

### Workflow: feature development

```bash
git issue create "Implement user authentication"
# -> Created issue j2dzt7xph12kswa9esn0

git checkout -b feature/auth
# ... make changes ...
git commit -m "Add login endpoint"

git issue link j2dz HEAD
git issue comment j2dz "Implemented basic auth, need to add OAuth"
git issue attach j2dz ./docs/auth-design.md

git issue close j2dz --commit HEAD
```

### Workflow: bug investigation

```bash
git issue for-commit abc123def          # what was this commit about?

git issue comment 7k2p "Root cause: race condition in cache"
git issue attach 7k2p ./debug-logs/

git issue link 7k2p def456abc
git issue close 7k2p --commit def456abc
```

### Workflow: umbrella issue tracking

```bash
git issue create "Implement user authentication system"   # -> 9xq4...
git issue create "Add login endpoint"                     # -> 3mb7...
git issue create "Implement JWT tokens"                   # -> 8fc1...

git issue relate 9xq4 3mb7      # umbrella tracks its sub-tasks
git issue relate 9xq4 8fc1
git issue blocks 3mb7 8fc1      # login lands before JWT

git issue show 9xq4             # the whole picture
```

## Implementation Notes

### Why XIDRs?

An issue ID has to be unique without anyone asking permission — two people can
file issues on a plane. That rules out a counter and points at a random or
timestamped identifier, which is fine to store and miserable to type.

An XID is 12 bytes: timestamp, machine, process, counter. An XIDR is the same
bytes reversed. Reversal is the whole trick: it puts the counter and machine
first, so the three or four characters someone actually types are already the
part that varies. Unreversed, every issue filed in the same second would share
its opening characters and no short prefix would resolve.

The sequential six-digit IDs this started with were pleasant to type and wrong to
merge: two clones both allocating `000042` produced two different issues with one
ID, and no merge could tell them apart.

### Why content-addressed comment names?

Comments were `discussion/001.md`, `002.md`, numbered by counting what was
already there. Two clones each adding a comment both wrote `003.md`, and the
force-push sync dropped one of them — silently, since both sides had a `003.md`.
The count also skewed whenever an attachment was present.

`discussion/<timestamp>-<hash>.md` cannot collide unless the content is
identical, in which case collapsing the two is the right answer anyway. The
timestamp keeps names sorting chronologically; `show` reads the timestamp inside
each comment rather than trusting the name.

### Why Tony format?

- Human-readable, and diffs like a text file
- Real types (null, arrays, objects) rather than string-typing everything
- Comments and metadata support
- It is this project's own format, and dogfooding it here is the point

### Why goldmark for `serve`?

Issue text is written by whoever filed the issue and is served from the same
origin as every other page, so raw HTML must not reach the browser. That
escaping is the part of a markdown renderer that is easiest to get subtly wrong,
so `serve` uses goldmark with its default (safe) renderer rather than a
hand-rolled subset: raw HTML becomes a placeholder comment and `javascript:`
link targets are refused. goldmark was already in this module's build graph as
an indirect dependency of `golang.org/x/tools`, so depending on it directly adds
no new module.

### Commit messages

Issue operations create commits with descriptive messages:

- `create: issue j2dzt7xph12kswa9esn0`
- `link: c477908`
- `label: added bug, urgent`
- `comment: First line of the comment...`
- `attach: design.md (1 file(s))`
- `close: closed by def456a`
- `import: update from directory`

### Git notes strategy

`refs/notes/issues` holds the reverse index. A commit's note is the IDs of the
issues that link it, one per line:

```
j2dzt7xph12kswa9esn0
7k2pd91xh12ksqm4bzn0
```

Appending is idempotent, so linking the same issue twice does not duplicate the
line.

## Limitations

- No merge for issue refs; sync is force-push and the last writer wins
- Read-only web UI (`git issue serve`), with no authentication; all edits go
  through the CLI
- One repository at a time: commands act on the repository containing the
  working directory
- No email notifications
- No advanced search (use `git log` and `git grep`)
- No user assignment (use git commit authorship)
- `branches` exists in the metadata but no command populates it
- Colors are written unconditionally, whether or not output is a terminal

## Future Enhancements

- Merge semantics for issue refs, which would unlock a writable `serve`
- Branch linking (the field exists; the command does not)
- Milestones
- `git issue cat` for viewing an attachment without exporting the issue
- GitHub/GitLab bridge

## License

See parent project license.
