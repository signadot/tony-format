# git issue push adds the moved-to ref and leaves the moved-from one, so a remote holds an issue as open and closed at once

`git issue push` writes the ref a status change moved TO and leaves the ref it moved FROM, so
a remote — and every clone of it — holds a state the source repository never has: an issue in
both `refs/issues/<id>` and `refs/closed/<id>` at once.

## Where it shows

Locally the invariant is clean. Closing MOVES the ref, so an issue has exactly one, and the
namespace is its status — which is what `issuelib`'s own doc says and what any consumer will
build on:

    after close:   refs/closed/<id>            (refs/issues/<id> gone)
    after reopen:  refs/issues/<id>            (refs/closed/<id> gone)

The verse tracker's remote, after ordinary use of `git issue close` and `git issue push`:

    17 refs/closed/*, 17 refs/issues/*, 22 unique ids
    12 of the 17 closed issues also carry a stale refs/issues

So a mirror clone (`+refs/*:refs/*`) sees twelve issues in two namespaces at once.

## Why it is worse than untidy

A consumer cannot recover the status from the refs, and cannot even tell WHICH case it is
looking at, because the ambiguity is symmetric: a closed-then-pushed issue and a
reopened-then-pushed issue both end up with an id in both namespaces. 'Prefer closed' fixes the
first and pins every reopened issue closed forever.

This cost verse's git-issue source nine of seventeen closed issues reading `status: open`,
and a charter acting on `{status: open}` therefore acting on closed issues. Fixed on the
verse side by reading the ref whose commit is the DESCENDANT — which works only because
`git issue close` commits the metadata change before moving the ref, so create/close/reopen
are commits on one chain. That is a good property and worth keeping deliberately; it is
currently the only thing that makes the situation recoverable at all.

## What would fix it

`push` should mirror the MOVE, not just the addition: push the new ref and delete the old one
on the remote in the same operation (`git push origin <new> :<old>`), so a remote's ref layout
equals the repository's. `pull` has the mirror-image question — a fetched close should remove
the local open ref rather than leaving both.

Worth checking whether `push --all` has the same gap, since that is the one an operator reaches
for after a batch of closes.

## Not a blocker

verse is fixed independently and does not need this to land — a source reading a repository it
does not control should not depend on the refs being tidy, and old mirrors and half-failed
pushes produce the same shape regardless. Filed because the invariant `git issue` maintains
locally is worth maintaining remotely: every other consumer of these refs will hit this, and
the ones that assume the local invariant will be quietly wrong rather than loudly.