# git-issue sync that does not lose a write

Issue: `w4mr5qphh12kr9f2nxn0`. `push` and `pull` move issue refs with force refspecs and
no compare anywhere in the path, so the last writer wins in whichever direction ran
last; `refs/issues/*` keeps no reflog, so what is overwritten is gone; and a sync that
failed reports success. The issue names four things, cheapest first, and this plan takes
them in that order: the first three are small and settle nothing contentious, and the
fourth has a decision in it.

## The model the fix moves to

Today a remote's issue refs are never held locally: `push` and `pull` write straight
between `refs/issues/*` here and `refs/issues/*` there, so nothing on either side can
say what the other holds without asking the network, and nothing can compare. Every
other git workflow keeps a tracking copy, and that is what makes fast-forward, ahead,
behind and diverged ordinary questions. So:

- `pull` fetches the remote's issue refs into **`refs/issue-remotes/<remote>/issues/*`**
  and `.../closed/*`, always with force -- a tracking ref is a copy and may be overwritten
  -- and then reconciles each issue locally, where ancestry is a local question.
- `push` fetches the tracking refs first, compares, and pushes **without force** the refs
  it has decided are safe. Git's own non-fast-forward rejection is then the backstop,
  not the policy.

An issue is one chain in whichever of the two namespaces it is in, so the comparison is
always of the two tips of the chain, wherever each side keeps it: local `refs/closed/X`
against remote `refs/issues/X` is a comparison of two commits, and the namespace follows
whichever tip wins. That is what `CleanupStaleRefs` approximates today by ancestry after
the fact; the reconcile does it before writing.

For each issue held on either side, the tips L (local) and R (remote) stand in one of
five relations, and each has one answer in each direction:

| relation | `pull` | `push` |
|---|---|---|
| remote only | create L = R | nothing |
| local only | nothing | create R = L |
| equal | nothing | nothing |
| R descends from L (behind) | fast-forward L to R, namespace as R has it | nothing |
| L descends from R (ahead) | nothing | push L, delete the counterpart R was in if it moved |
| neither (diverged) | **refuse**, unless told which side wins (step 3), or merge (step 4) | **refuse**, same |

`refs/notes/issues` is one ref for the repository and diverges whenever two clones link
commits. Git merges notes natively: `git notes merge -s union` over the tracking copy is
the whole answer, and it belongs in step 3, not step 4, because it costs nothing to
decide.

## Step 1: a reflog for issue refs

`core.logAllRefUpdates=true` logs `refs/heads/`, `refs/remotes/`, `refs/notes/` and
`HEAD` and nothing else, and this repository's `.git/logs/refs/` holds `heads notes
remotes stash`. An overwritten issue is a dangling commit with nothing pointing at it.

`git update-ref --create-reflog` creates the log for the ref it writes, and git keeps
logging any ref whose log exists. So `setRef` and `MoveRef` pass it, and every ref this
tool writes is logged from then on. A ref that first arrived by fetch has no log until
the first local write creates one, which is early enough: the transition that loses
something is a later overwrite, and by then the log exists. Not `core.logAllRefUpdates =
always`: that is a repository-wide setting the tool would be changing under its user,
for refs that are not its own.

design.md's "recoverable only by someone who knows to go looking in the reflog" becomes
true, and says so. A test overwrites an issue ref and finds the old tip in
`git reflog show <ref>`.

## Step 2: a failed sync fails

`Push` and `Fetch` keep trying every refspec, and answer an error naming the ones that
failed, with git's words. `push` and `pull` print it and exit non-zero. The "not
found" cases that are silent today stay silent: a deletion of a ref the remote does not
have, a fetch of `refs/closed/*` from a remote with none. A test pushes at a remote that
refuses and sees the error.

## Step 3: refuse a divergent sync

The tracking namespace, the table above, and `--force` on both commands, in the git
sense: `pull --force` takes the remote's tip for every diverged issue, `push --force`
takes the local one. Without it, a diverged issue is named -- id, title, and the two tips
-- and skipped; every other issue syncs; and the command exits non-zero, so a script
learns that something was left undone and a person learns which issue to look at. This
is the step that turns a silent overwrite into a decision, and it needs no rule for
`status`.

`push --all`'s counterpart deletion becomes a consequence of the table rather than an
unconditional sweep: it deletes `refs/issues/X` on the remote only when the local
`refs/closed/X` descends from what the remote holds, which is the case where the close
is a fast-forward of the remote's open chain. A reopen on the remote makes the two
diverge, and is refused with the rest.

`pull` no longer needs `CleanupStaleRefs` for what it fetched, since the reconcile puts
an issue in one namespace as it goes; it stays for repositories already holding a pair.

Notes: `pull` fetches `refs/notes/issues` to a tracking ref and merges it with
`git notes merge -s union`; `push` pushes the merged ref without force. A link made in
another clone survives the next `push --all`, which fixes the "sharper edge" design.md
names.

Tests, over the bare-origin harness `push_test.go` already has: a second working clone
against the same origin; each row of the table in each direction; `--force` on each;
the reopen-on-the-remote case; notes linked from both clones both present after a sync
each way; and `TestPull_AdoptsStatusMove` still passing.

## Step 4: merge a diverged issue

What is diverged is two chains from one root, each holding commits that rewrote
`meta.tony` and added files under `discussion/`. `git merge-tree --write-tree base ours
theirs` (git 2.38 and later; this machine has 2.50) three-way merges the trees:
`discussion/` unions by construction, since names are `<ts>-<hash>.md` and never collide;
`description.md` merges as text, and a real conflict there is refused, as it should be,
since two people rewrote the same sentence. `meta.tony` is merged by this tool rather
than by lines: parse the three, and

- **list fields** -- `commits`, `branches`, `labels`, the four relations -- union, in
  the order ours then theirs' additions;
- **`created`** is the base's; **`updated`** is the later of the two;
- **`status` and `closed_by`** need a rule, which is the decision in this plan.

The merged tree is committed with both tips as parents, so the chain records the merge
and the next reconcile sees a fast-forward on both sides.

**The `status` rule.** Options, with what each gets wrong:

1. *Closed wins.* Simple and what `CleanupStaleRefs` does for a pair with no ancestry.
   Wrong for the case the issue raises: someone reopened on purpose and the merge closes
   it again.
2. *The later status change wins*, by the commit date of the commit that last changed
   `status` on each side, found by walking each chain back from its tip to the merge
   base. Right for the reopen case and for the ordinary close; wrong only when two clocks
   disagree by more than the gap between two people acting on one issue, which is the
   same trust every git log already places in commit dates. Ties close.
3. *Refuse.* Everything else merges and a status conflict is named for a person. Safe,
   and a status conflict is rare enough that asking is not a burden -- but it means step
   4 still leaves a case step 3 already handles by refusing, which is not much of a step.

Recommendation: 2, with `closed_by` following `status` -- the closing side's value when
the merge closes, cleared when it reopens. Each is a few lines once the walk exists,
and the walk is `git log --format=%H base..tip -- meta.tony` read back through
`GetByRef`.

**Writable `serve`** is what this unlocks, and is not in this plan.

## What to touch

- `issuelib/git_store.go`: `--create-reflog` in `setRef` and `MoveRef`; `Push` and
  `Fetch` answering errors; `TrackingRef(remote, ref)`; `Relation(local, remote) `; the
  reconcile; `MergeIssue(base, ours, theirs)` in step 4; notes merge.
- `issuelib/store.go`: the interface, for the commands and the tests.
- `commands/push.go`, `commands/pull.go`: the table, `--force`, the report of what was
  refused, the exit status. `staleRemoteRefs` and `deletions` fold into the reconcile.
- `commands/push_test.go`: a second clone in the harness, and the tests above.
- `design.md`: "Sync, and what it costs" rewritten around the tracking namespace and the
  table; the reflog sentence corrected; "Known defects" loses its first entry when step
  4 lands and gains nothing.
- `README.md`: `--force` on `push` and `pull`.

## Steps

1. Reflog. One commit.
2. Errors. One commit.
3. Tracking refs, the table, `--force`, notes merge, docs. One commit, possibly two.
4. The merge, once the `status` rule is settled. One commit.
5. Close the issue with the merge.

## Observed

This repository, 2026-09-20, before any of it: 352 issue refs locally and 347 on
origin, standing thus by the table's relations:

| relation | issues |
|---|---|
| equal | 346 |
| local ahead | 1 |
| local only | 5 |
| behind | 0 |
| diverged | 0 |

So a sync is safe here today, as the issue found for verse. The six unpushed are one
edit in another clone away from being the first loss.
