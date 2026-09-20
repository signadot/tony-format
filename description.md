# git-issue: push and pull are force-only, so sync discards the local store's compare-and-swap -- and refs/issues keeps no reflog, so what is overwritten is gone

`push` and `pull` both move issue refs with force refspecs and no merge anywhere
in the path, so the last writer wins in whichever direction ran last. This is
already the design's own largest known defect — design.md "Sync, and what it
costs" and "Known defects" both name it, and name the fix: a three-way merge over
`meta.tony` (list fields union; `status` and `closed_by` need a rule) plus a union
merge for notes via `notes.mergeStrategy`. This issue is to track doing it, and to
record three things the design document does not.

## What the local store already guarantees, and the transport throws away

`GitStore` writes every issue with `git update-ref <ref> <new> <old>`
(`issuelib/git_store.go:141`) — a compare-and-swap — and `retryRefMoved`
(`git_store.go:166`) re-attempts up to 32 times when the ref moved underneath.
Two processes editing one issue in one clone cannot lose a write.

`Fetch` and `Push` (`git_store.go:568`, `git_store.go:551`) then shell out to
`git fetch` / `git push` with `+refs/issues/*:refs/issues/*` and friends, which
is precisely a compare-and-swap with the compare removed. The property the store
spends a retry loop to hold is unavailable across two clones — which is the only
place it is hard to hold and the only place it matters.

## 1. There is no reflog to recover from

design.md says the loser's commits are "recoverable only by someone who knows to
go looking in the reflog." There is no reflog. `core.logAllRefUpdates=true` — the
default, and what verse has set — logs updates only for `refs/heads/`,
`refs/remotes/`, `refs/notes/` and `HEAD`. `refs/issues/*` and `refs/closed/*` get
none:

    $ git config core.logAllRefUpdates
    true
    $ ls .git/logs/refs/
    heads  notes  remotes

So an overwritten issue is a dangling commit with nothing pointing at it and no
record that it was ever there: recoverable only via `git fsck --lost-found`, only
by someone who already suspects a loss, and only until gc prunes it. The sentence
in design.md should be corrected either way, since it is the line a reader would
rely on after losing a comment.

`core.logAllRefUpdates = always` logs every ref, and git also keeps a log for any
ref whose log file already exists — so either setting it at `init`/first write, or
creating the log files, makes force-overwrite survivable. That is worth doing on
its own, ahead of the merge, because it is the difference between "the last writer
won" and "the loser's work is gone."

## 2. A failed sync is not an error

`Push` and `Fetch` print `Warning: failed to ...` to the store's `out` and return
`nil` — both loops discard git's exit status entirely. `pull` then prints
`Done. N issue(s) in local repository.` and exits 0. A sync that pushed nothing
because the remote rejected it is indistinguishable, to a script or a caller, from
one that worked; the only signal is a warning line in the middle of normal output.
Whatever happens to the merge, these two should return what git told them.

## 3. `push --all` deletes the counterpart ref on the remote

`staleRemoteRefs` (`commands/push.go:141`) deletes the remote's counterpart of
every local issue: close X locally and the next `push --all` deletes
`refs/issues/X` on the remote. That is right for the case it is written for — an
issue must not be open and closed at once — but it is also unconditional. If
someone else reopened X on the remote, this deletes their reopen and force-pushes
my closed ref over it, and nothing on either side reports a conflict. A merge rule
for `status` would decide this; until there is one it is worth a mention in
design.md beside the notes-ref hazard, which is documented.

## What "credible" could mean, cheapest first

1. **Keep a reflog for issue refs** (see 1). Does not prevent a loss, makes every
   loss recoverable by someone who knows the trick. Small.
2. **Return git's exit status** from `Push`/`Fetch` (see 2). Small.
3. **Refuse a divergent sync unless forced.** Before writing, compare each local
   ref against the remote's: fast-forward silently, no-op when equal, and on a
   genuine divergence name the issue and stop, with `--force` to say "take theirs"
   / "take mine". This turns a silent overwrite into a decision without needing
   merge semantics at all, and would cover most of the real exposure.
4. **The three-way merge** design.md describes. `meta.tony`'s list fields union;
   `status`/`closed_by` want a rule (last-closer-wins is defensible, or reopen
   beats close, or refuse and ask); `discussion/` already has collision-free names
   (`<ts>-<hash>.md`) so it unions by construction; notes take
   `notes.mergeStrategy=union`. This is the one that unlocks a writable `serve`.

3 is the one that changes the outcome soonest — it is the only item that stops a
loss rather than recording or repairing it, and it does not depend on settling how
`status` merges.

## Observed

Not a hypothetical in only one sense: verse currently holds 199 issue refs, 168
identical to origin, 31 local-only and unpushed, 0 divergent — so a pull there is
safe *today*, and would silently discard any of the 31 the moment one is pushed by
one clone and commented on by another. The exposure grows with the number of
people who sync, which is the direction the tool is going.