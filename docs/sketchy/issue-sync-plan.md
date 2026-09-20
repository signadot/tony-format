# git-issue sync that does not lose a write

Issue: `w4mr5qphh12kr9f2nxn0`. Branch: `issue-w4mr5qph`, worktree
`.claude/worktrees/issue-sync`. Code: `go-tony/cmd/git-issue/` (`issuelib/` is the store,
`commands/` the CLI). Design of record: `go-tony/cmd/git-issue/design.md`.

This plan is written to be executed by an agent that has not seen the discussion behind
it. Everything decided is stated; where something is left to judgment it says so. Read
"Rules for whoever executes this" before touching anything.

## The problem

`push` and `pull` move issue refs with force refspecs (`+refs/issues/*:refs/issues/*`)
and nothing compares before writing, so the last writer wins in whichever direction ran
last. Locally the store is careful -- every write is `git update-ref <ref> <new> <old>`,
a compare-and-swap, retried when the ref moved (`issuelib/git_store.go`, `setRef`,
`retryRefMoved`) -- and the transport throws that away across clones, which is the only
place it is hard and the only place it matters. Three more facts from the issue:

1. **No reflog.** `core.logAllRefUpdates=true` logs `refs/heads/`, `refs/remotes/`,
   `refs/notes/` and `HEAD` only. An overwritten issue ref is a dangling commit: gone
   at the next gc, findable before then only by `git fsck --lost-found`.
2. **A failed sync is not an error.** `GitStore.Push` and `GitStore.Fetch` print
   `Warning: ...` and return nil; `pull` then prints `Done.` and exits 0.
3. **`push --all` deletes the counterpart ref unconditionally.** Close X locally, and
   the next `push --all` deletes `refs/issues/X` on the remote even if someone reopened
   it there with new commits.

## Decisions taken

These were settled in discussion with the repository's owner. Do not reopen them; if one
turns out to be unworkable, stop and report rather than substituting another.

1. **Issues move to a new ref namespace with a generation in it.** Old clients name
   their refspecs literally, so they cannot see or touch a ref they do not name: the move
   makes them harmless rather than merely detectable. The generation is `v1`. It changes
   only for a break old clients cannot coexist with -- a ref layout or sync protocol
   change, which is what this is. A change to what `meta.tony` holds is not one, and
   never moves the namespace.
2. **The old namespace becomes a tripwire.** Once a remote has been synced by a new
   client it holds no old-namespace refs. One appearing there later was pushed by an old
   client, definitively, and a new client says so and keeps the work.
3. **An old client cannot be refused**, only made harmless and reported: the remote is a
   plain git server with no hook of ours.
4. **No separate migration command.** A new client adopts old-namespace refs on sight,
   locally, and an ordinary `push` migrates the remote as a consequence of the same rule
   that decides every other push. Commits are never rewritten: a ref is recreated at the
   same commit, so every recorded SHA, `Issue:` trailer and issue id keeps resolving.
5. **A tracking namespace for the remote's refs**, as every other git workflow has, so
   that ahead, behind and diverged are local ancestry questions.
6. **Diverged issues are refused, then merged.** Step 4 refuses them unless `--force`
   says which side wins; step 5 merges them three-way.
7. **The `status` rule for a merge: the later status change wins**, by the committer
   date of the commit that last changed `status` on each side since the merge base; a
   tie closes. `closed_by` follows: the closing side's value when the merge closes,
   cleared when it opens. (Client version is admission control -- whether a write may be
   trusted -- and says nothing about what its author meant, so it does not decide
   `status`.)
8. **Nothing is added to `meta.tony`.** A schema field with one legal value tests
   nothing; add one when a second value exists.

## The refs, exactly

Generation `v1`. In code the old namespace is called **gen0**, never "legacy":
`IsLegacyRef` already means "six-digit numeric id", which is a different thing.

| what | ref |
|---|---|
| open issue | `refs/git-issues/v1/open/<xidr>` |
| closed issue | `refs/git-issues/v1/closed/<xidr>` |
| reverse index (commit -> issue ids) | `refs/notes/git-issues/v1` |
| tracking, open | `refs/git-issues/v1/remotes/<remote>/open/<xidr>` |
| tracking, closed | `refs/git-issues/v1/remotes/<remote>/closed/<xidr>` |
| tracking, gen0 open | `refs/git-issues/v1/remotes/<remote>/gen0-open/<xidr>` |
| tracking, gen0 closed | `refs/git-issues/v1/remotes/<remote>/gen0-closed/<xidr>` |
| tracking, notes | `refs/notes/git-issues/remotes/<remote>/v1` |
| tracking, gen0 notes | `refs/notes/git-issues/remotes/<remote>/gen0` |
| gen0 open / closed / notes | `refs/issues/<xidr>`, `refs/closed/<xidr>`, `refs/notes/issues` |

Notes refs stay under `refs/notes/` because that is where git logs them by default and
where `git log --notes=` looks. `refs/notes/git-issues/v1` is a leaf, so nothing may
live under it; that is why tracking notes are at `.../remotes/<remote>/v1` and not under
`v1/`. Verified with git 2.50: `git notes --ref=refs/notes/git-issues/v1 merge -s union
refs/notes/git-issues/remotes/origin/v1` concatenates both sides' notes and leaves the
merged ref a descendant of the other, so it pushes without force.

`refs/meta/issue-counter` allocates nothing any more. Drop it from every refspec and
leave any existing one alone.

Tracking refs are never pushed: `push` names `open/*` and `closed/*` explicitly.

## The model

**An issue is one chain, wherever its tips are held.** For one issue id, a clone can hold
a local tip L (open or closed) and, after a fetch, up to four remote tips: open, closed,
gen0-open, gen0-closed. Status is which namespace a tip is in. All comparison is of
commits by ancestry (`git merge-base --is-ancestor A B`: A is an ancestor of B, or equal).

**The remote's tip R** is the remote tip that every other remote tip is an ancestor of.
If two remote tips are the same commit in an open and a closed namespace, R is closed
(the existing `CleanupStaleRefs` rule). If no remote tip descends from all the others
the remote itself is split, which only an old client pushing after a new one can cause;
treat the issue as **diverged**.

**The verdict**, from L and R:

| verdict | meaning | `pull` | `push` |
|---|---|---|---|
| `RemoteOnly` | no L | create L at R, in R's status namespace | nothing |
| `LocalOnly` | no R | nothing | make the remote right (below) |
| `Equal` | same commit | fix L's status namespace if it differs from R's | make the remote right |
| `Behind` | L is an ancestor of R | move L to R, in R's status namespace | nothing |
| `Ahead` | R is an ancestor of L | nothing | make the remote right |
| `Diverged` | neither, or the remote is split | refuse; with `--force` take R; after step 5, merge | refuse; with `--force` overwrite; after step 5, merge then push |

**"Make the remote right"** means: the remote ends with exactly one ref for the issue,
the `v1` ref in L's status namespace, at L. Create or fast-forward that ref, then delete
every other remote ref for the issue (the counterpart namespace, both gen0 refs). That is
safe exactly when the verdict is `LocalOnly`, `Equal` or `Ahead`, because then L descends
from every remote tip. This one rule replaces `staleRemoteRefs`, fixes fact 3 above, and
is how a remote is migrated: the first new `push --all` moves every issue into `v1` and
empties gen0, with no command of its own.

**Every write to the remote carries a lease**, which is the compare-and-swap the
transport lacked: `git push --force-with-lease=<ref>:<expected-sha>` (`<ref>:` with
nothing after the colon means "must not exist"). The refspec is non-force (`src:dst`)
for a create or fast-forward, `:dst` for a delete, and `+src:dst` only under `--force`
on a diverged issue. `<expected-sha>` is what the tracking ref says. A lease that fails
means someone pushed between our fetch and our push: report the issue, leave it, exit
non-zero; a re-run fetches again and decides again.

**Old-client detection.** A remote is *migrated* when the fetch found at least one `v1`
issue ref or the `v1` notes ref on it. On a migrated remote, an issue with a gen0 remote
tip was pushed by an old client. The work is kept -- a gen0 tip is a tip like any other in
the verdict -- and at the end of the command one paragraph names the issues and says: a
git-issue older than this one pushed these to `<remote>`; nothing was lost; ask whoever
pushed to upgrade. On an unmigrated remote, gen0 refs are just where the issues are, and
nothing is said.

**Local adoption.** The local store never holds a gen0 ref after a command has run. At
first use per process, for each local gen0 issue ref at tip G: no `v1` ref for the id ->
create one at G in the matching status namespace and delete the gen0 ref; a `v1` ref at
V with G an ancestor of V (or equal) -> delete the gen0 ref; V an ancestor of G -> move
the `v1` ref to G (status as the gen0 ref had it) and delete the gen0 ref; neither ->
leave both and warn, naming the issue (step 5's merge resolves it). The local gen0 notes
ref is union-merged into `refs/notes/git-issues/v1` (or becomes it, if there is none)
and deleted. Every create and delete is a compare-and-swap. One line reports what was
adopted; nothing is printed when there was nothing to adopt.

A person who alternates old and new binaries on one clone converges: the old one's pull
re-creates gen0 refs from an unmigrated remote, and the new one adopts them again.

**Notes.** `pull` and `push` both fetch the remote's `v1` and gen0 notes refs to their
tracking refs and union-merge each into the local `v1` notes ref. `push` then pushes the
local notes ref without force, leased on the tracking value, and deletes the remote's
gen0 notes ref, leased, since the merged ref descends from it.

## Rules for whoever executes this

- **Do not install the binary you build, and do not run it in this repository or any
  real clone.** This repository tracks its own issues with the *installed* `git issue`,
  and a session hook runs `git issue list` at startup. A new binary adopts the local
  refs out from under the installed one. All testing is in `t.TempDir()` repositories
  with bare `t.TempDir()` remotes. Rollout is the owner's, and is described at the end.
- You will use the installed `git issue` to comment on and close the issue. That is the
  old binary and that is fine.
- Work in the worktree, on the branch. One step, one commit (two where a step says so).
  Tests green at every commit: from `go-tony/`, `go test ./cmd/git-issue/...`, then
  `go vet ./cmd/git-issue/...` and `gofmt -l cmd/git-issue`. Before the final merge run
  `go test ./...` from `go-tony/` once.
- The tests `t.Chdir` into a scratch repository and the store has no path of its own:
  **never `t.Parallel()`** in these packages.
- Commit messages: a subject that states what is now true (look at `git log` for the
  house style), a body that says what was wrong and what the rule is now, then
  `Issue: w4mr5qphh12kr9f2nxn0`, then the attribution line your session specifies.
- Comments say why, in the register of the surrounding code. Do not leave a comment that
  narrates the change ("now we...", "previously..."), except where the old behaviour is
  the reason for the code, as the existing comments on `setRef` do.
- Keep each change to its delta. Do not rename, reorder or "tidy" code a step does not
  need. `migrate` (numeric ids to XIDRs) is known to be non-idempotent; leave that alone
  and only replace its ref literals with the helpers.
- zsh globs: quote every refspec and pattern in a shell command (`'refs/issues/*'`).
- A `git fetch` refspec naming one ref that the remote lacks fails the whole fetch with
  `couldn't find remote ref`. Glob refspecs that match nothing do not. So: globs
  together in one fetch, single refs (the two notes refs) one at a time, tolerating that
  message.
- If a step's tests cannot be made to pass without departing from "Decisions taken" or
  "The model", stop and report what you found. Do not improvise a different model.
- design.md describes the tracker **as built**. Each step moves the text for what it
  built out of "Designed but not built" and into the body. README.md likewise.

## Step 1: a reflog for issue refs

`setRef` (`issuelib/git_store.go`) runs `git update-ref <ref> <new> <old>`. Add
`--create-reflog`. Git then logs that ref from that write on, and goes on logging any
ref whose log exists, which covers a later overwrite by fetch. `MoveRef` creates through
`setRef`, so it is covered. Do not set `core.logAllRefUpdates`: that is a
repository-wide setting changed under the user, for refs that are not ours.

Test (`issuelib/setref_test.go` has the harness): create an issue, update it twice, and
find all three tips in `git reflog show <ref>`; overwrite the ref with `git update-ref`
directly and find the overwritten tip still in the reflog.

design.md, "Sync, and what it costs": the sentence "recoverable only by someone who
knows to go looking in the reflog" is false today and becomes true with this step;
rewrite it to say what is now the case, including that a ref which has only ever been
fetched has no log until its first local write.

## Step 2: a failed sync fails

`Push` and `Fetch` keep attempting every refspec. They collect the failures and return
one error naming each refspec and git's message (`errors.Join`, or a small type with a
list). The cases silent today stay silent and are not failures: `does not match any`,
`remote ref does not exist`, `couldn't find remote ref`. `pushAll`, `pushSingle` and
`pull.run` return the error, so the process exits non-zero; they still print what
succeeded. `pushSingle` ignores the error from its notes push (`_ =`); stop ignoring it.
Update the `Store` interface comments, which promise the old behaviour.

Tests (`commands/push_test.go`): push at a remote whose URL is a nonexistent path ->
error, and the error names the refspec; pull likewise; a pull from a remote with no
closed issues -> no error.

## Step 3: the `v1` namespace, and adoption

New file `issuelib/namespace.go`: the constants and functions for every row of "The
refs, exactly" (`Generation`, `OpenPrefix`, `ClosedPrefix`, `NotesRef`, `Gen0OpenPrefix`,
`Gen0ClosedPrefix`, `Gen0NotesRef`, and `TrackingOpenPrefix(remote)` and its five
siblings). `issuelib/format.go`: `RefForXIDR`, `ClosedRefForXIDR` answer `v1` refs;
`IsClosedRef` is true for both closed prefixes; `XIDRFromRef` accepts all four local
prefixes; add `IsGen0Ref(ref)`. `StatusFromRef` follows from `IsClosedRef`.

`issuelib/git_store.go`: `ListRefs`, `FindRef`, `CleanupStaleRefs` and anything else
that names `refs/issues/*` or `refs/closed/*` use the `v1` prefixes; `AddNote` and
`GetNotes` use `NotesRef`. Add `adoptGen0()` as "Local adoption" describes, run once per
`GitStore` (a `sync.Once`) at the top of `ListRefs` and `FindRef`, which everything that
reads an existing issue goes through. It must be a no-op outside a git repository and
must not fail the command it rides on: a failure to adopt is a warning on `Out`.
Deletes are `git update-ref -d <ref> <expected>`. Notes: if `NotesRef` is absent,
`git update-ref --create-reflog NotesRef <gen0 commit> <zero>`; otherwise
`git notes --ref=<NotesRef> merge -s union refs/notes/issues`; then delete the gen0
notes ref, compare-and-swap.

`commands/push.go`, `commands/pull.go`: refspecs become the `v1` ones, **still with
force**, and drop `refs/meta/issue-counter`; `pull` additionally fetches the gen0
refspecs as it does today (into the gen0 local namespace) and then triggers adoption, so
a new client still sees an unmigrated remote's issues. `push` does not yet touch the
remote's gen0 refs. This step leaves the force hazard exactly as it was among new
clients; step 4 removes it. `commands/migrate.go`: ref literals -> helpers. Comments,
usage text in `commands/root.go`, `main.go` and `issuelib/doc.go`: the new paths.

Tests: a repository seeded with gen0 refs by raw `git update-ref` (open, closed, and a
gen0 notes ref) lists, shows and edits every issue; afterwards no gen0 ref remains and
each `v1` ref is at the commit its gen0 ref was; adoption of an issue that also has a
`v1` ref, in each of the four relations; adoption outside a repository does nothing;
`for-commit` answers from the merged notes; every existing test passes with the new
paths (several name `refs/issues/` literally -- switch them to the helpers).

## Step 4: tracking refs, the verdict, leases, `--force`

Two commits are fine: the store half with its tests, then the commands.

**Store** (`issuelib/sync.go`, new; interface additions in `store.go`):

```go
// FetchTracking refreshes every tracking ref for remote: one fetch, with --prune, of the
// four glob refspecs (forced: a tracking ref is a copy), then the two notes refs one at
// a time. A notes ref the remote lacks deletes its tracking ref. Reports whether the
// remote is migrated.
FetchTracking(remote string) (migrated bool, err error)

// PlanSync reads local and tracking refs only -- no network -- and answers one plan per
// issue id held on either side, sorted by id.
PlanSync(remote string) ([]IssuePlan, error)

type Tip struct {
    Ref    string // full ref name
    Commit string
    Closed bool
    Gen0   bool
}
type Verdict int // RemoteOnly, LocalOnly, Equal, Behind, Ahead, Diverged
type IssuePlan struct {
    XIDR      string
    Local     *Tip   // nil when not held locally
    Remote    []Tip  // every remote tip, from the tracking refs
    R         *Tip   // the remote's tip; nil when there is none or the remote is split
    Verdict   Verdict
    Split     bool   // the remote's own tips diverge
    OldClient bool   // a gen0 remote tip on a migrated remote
}

// ApplyPull and ApplyPush carry out one plan. force applies to Diverged only. They
// answer what they did, for the command to print, and an error for a lease or a
// compare-and-swap that failed.
ApplyPull(p IssuePlan, force bool) (string, error)
ApplyPush(remote string, p IssuePlan, force bool) (string, error)

// SyncNotes merges the tracking notes into NotesRef; with push set it then pushes
// NotesRef leased and deletes the remote's gen0 notes ref.
SyncNotes(remote string, push bool) error
```

`PlanSync` is a pure function of refs and ancestry. Keep it that way: it is what the
table-driven tests exercise, and what `--dry-run` prints. `ApplyPush` issues one `git
push` per issue carrying every refspec and lease for that issue, so an issue's remote
refs change together or not at all (`--atomic`).

**Commands.** `pull [--force] [--dry-run] [remote]`: `FetchTracking`, `PlanSync`, apply
each, `SyncNotes(remote, false)`, then the summary. `push [--all] [--force] [--dry-run]
<id> [remote]`: the same with `ApplyPush`, over every plan or the one id; an id not held
locally is an error as now. The summary, in this order: counts by what was done; each
refused issue on its own line with id, title, and both short SHAs, and the sentence that
`--force` takes the remote's (pull) or the local (push) and that the other tip stays in
the reflog; the old-client paragraph when any plan had `OldClient`; then `Done.` The
exit status is non-zero when anything was refused or failed. `--dry-run` prints each
plan's verdict and intended action and writes nothing, the fetch of tracking refs aside.

Delete `staleRemoteRefs`, `counterpartRef` and `deletions` from `commands/push.go`;
"make the remote right" replaces them. `pull` stops calling `CleanupStaleRefs` for what
it fetched, since `ApplyPull` leaves an issue in one namespace; keep the call for
repositories already holding a pair, and keep the function.

**Tests.** Extend the harness in `commands/push_test.go` with a second working clone of
the same bare origin (a helper that returns a directory; a test acts as clone B by
`t.Chdir` into it and building its own store). Then:

- `PlanSync`, table-driven over raw refs: every verdict; R chosen correctly among two,
  three and four remote tips; same commit open and closed -> closed; a split remote ->
  `Diverged` with `Split`; `OldClient` only on a migrated remote.
- Each verdict end to end, both directions, asserting the refs on both sides afterwards.
- The loss this issue is about: A and B both comment on one issue; A pushes; B's pull
  refuses, names the issue, exits non-zero, and B's comment is still on B's ref; B's
  push refuses likewise; `pull --force` takes A's and B's old tip is in B's reflog.
- Fact 3: A closes; B reopens and comments and pushes; A's `push --all` refuses the
  issue and B's reopen is still on the remote.
- Migration by push: an origin seeded with gen0 refs only; a new clone pulls, sees every
  issue, pushes `--all`; the origin then holds only `v1` refs and the `v1` notes ref,
  each at the commit its gen0 ref was.
- The tripwire: after that, a raw `git push origin '+refs/issues/X:refs/issues/X'` from
  a clone, standing in for an old client, with one new commit -> the next pull adopts
  the commit, reports an old client, and the next push empties gen0 again. The same
  with a stale gen0 ref (no new commit) -> reported, nothing adopted, gen0 emptied.
- A lease that fails: between B's fetch and B's push, move the remote ref by hand (call
  `FetchTracking`/`PlanSync`, then push from A, then `ApplyPush` from B) -> error naming
  the issue, the remote unchanged by B.
- Notes: A and B each link a different commit to different issues; after each has
  pulled and pushed, `for-commit` answers both on both sides.
- `TestPush_MirrorsStatusMove`, `TestPushAll_MirrorsStatusMove`,
  `TestPushAll_LeavesIssuesItDoesNotHave` and `TestPull_AdoptsStatusMove` still pass,
  rewritten only where they name removed functions.

## Step 5: merge a diverged issue

`issuelib/merge.go`, new. `MergeIssue(base, ours, theirs string) (commit string, err
error)`:

1. `git merge-tree --write-tree --merge-base=<base> <ours> <theirs>` (git 2.38 or
   later; if the flag is missing, fail with a message that says so). `discussion/`
   unions by construction -- names are `<ts>-<hash>.md` and do not collide. A conflict
   in `description.md` or any other path but `meta.tony` is an error naming the path:
   two people rewrote the same text, and that is a person's to settle.
2. `meta.tony` is merged by value, never by lines. Read the three with `GetByRef`-style
   parsing. List fields (`commits`, `branches`, `labels`, `related_issues`, `blocks`,
   `blocked_by`, `duplicates`): ours, then theirs' entries not already present, in
   their order. `id` and `created`: the base's. `updated`: the later.
3. `status` and `closed_by`: decision 7. Find, on each side, the newest commit in
   `base..tip` whose `meta.tony` `status` differs from its first parent's; the side
   whose such commit has the later committer date decides; a side with none does not
   compete; neither side having one leaves the base's; equal dates close. `closed_by`
   is the deciding side's when the result is closed, absent when open.
4. Write the merged `meta.tony` blob into the merged tree (temporary index, as
   `updateCommitOnce` does), `git commit-tree <tree> -p <ours> -p <theirs>` with the
   message `merge: <short ours> <short theirs>`, and answer the commit.

`ApplyPull` on `Diverged` without `--force`: merge L and R, then move L to the merge
commit, in the merged status's namespace. `ApplyPush` on `Diverged`: merge, move L, then
the plan is `Ahead` and pushes as one. A split remote merges its tips pairwise first. A
merge that errors (a text conflict) falls back to step 4's refusal, with the path in the
message. `--force` still means what it meant. Local adoption's "neither -> leave both
and warn" case merges instead.

Before writing any of it, check what walks a chain linearly and would misread a merge
commit: `grep -rn '"log"\|rev-list\|--first-parent' cmd/git-issue`. `migrate-comments`
and `serve`'s history view are the suspects. Make each either follow first parents or
handle both, and say which in its comment.

Tests: lists union in order; each branch of the status rule, with commit dates set by
`GIT_COMMITTER_DATE`; both comments present after a merge; the merge commit has two
parents and both clones fast-forward to it on their next sync; a `description.md`
conflict refuses and names the path; an issue read through `GetByRef` at a merge commit
is whole; whatever the grep above found still works across a merge.

## Step 6: documents, and closing

design.md by now describes all of it as built. Check these in particular: "Storage
model" shows the `v1` refs and says what the generation is for and when it changes;
"Status is the namespace" names the new prefixes; "Sync, and what it costs" is rewritten
around tracking refs, the verdict table, leases, adoption and the tripwire, keeps the
history of why force was there, and says what mixed versions mean (next section);
"Known defects" loses "No merge for issue refs"; "Designed but not built" loses the sync
entry and "Merging issue refs"; the non-goal about a writable web UI no longer blames
the sync model. README.md: the ref paths, `--force` and `--dry-run` on both commands,
the sync section, the limitations list. Grep both for `refs/issues`, `refs/closed`,
`force` and `last writer`.

Merge to `main` with `--no-ff` and a message in the style of the recent merges; close
the issue with `git issue close w4mr5qph --commit <merge>`. Do not push, tag or install.

## Mixed versions, and rollout

For design.md, and for the owner:

- A new client cannot be hurt by an old one on a migrated remote: the old one cannot
  name a `v1` ref. What an old client pushes lands in gen0, is adopted by the next new
  client to sync, and is reported.
- An old client on a migrated remote sees its own stale local copy -- a fetch that
  matches nothing deletes nothing -- and no new issues. It is never told why; the people
  told are the new clients' users, who can pass it on.
- Until a remote's first new `push`, new and old clients share gen0 on it and the old
  hazards apply to old clients' pushes as before.
- Rollout, per repository: install the new binary everywhere that syncs (people,
  agents, CI, session hooks); `git issue pull --dry-run` and read the verdicts; `git
  issue pull`; `git issue push --all`, which migrates the remote. There is nothing to
  undo on failure before that push, and after it the gen0 refs' commits are the `v1`
  refs' commits.

## Observed

This repository, 2026-09-20, before any of it: 352 issue refs locally, 347 on origin.

| relation | issues |
|---|---|
| equal | 346 |
| local ahead | 1 |
| local only | 5 |
| behind | 0 |
| diverged | 0 |

A sync is safe here today, as the issue found for verse (199 refs, 31 unpushed, none
diverged). The unpushed are one edit in another clone away from being the first loss.
