# logd: a scope may store an operation relative to a base that moves, and becomes unreadable when it does

Baseline is safe from this and a scope is not, for the reason api/storage_context.go already
gives: "a scope's base MOVES underneath it (baseline advances), so a relative op stored in a
scope layer recomputes against something other than what it was written against. Baseline gets
away with arbitrary ops only because its replay is deterministic."

Verifying a patch applies before storing it (8e1b334) closes this for baseline completely: the
base a baseline delta replays against is the same base forever, so a delta that applied once
applies always. A scope's does not, and the check cannot see the future.

## Reproduce

```go
// baseline: {s: bob}
scopedCommit(s1, "s", `!replace {from: bob, to: rob}`)   // verifies and commits; scope reads s: rob
baselineCommit("s", `someone-else`)                      // baseline moves the leaf

ReadStateAt("", commit, &s1)
//  -> failed to apply patches: replace patching "someone-else"
//     gave # node at $.from differes from replacement from: at $.to
ReadStateAt("", commit, nil)  // baseline: fine
```

The scope is now unreadable and stays that way: the delta is stored, and every read of that
scope replays it. Baseline is untouched, and DeleteScope is the only repair -- which is to say
the sandbox is gone.

## What closes it

The storage vocabulary, applied to SCOPED writes. It is what the vocabulary is for, and it is
already written down and already enforced on the one thing logd builds itself
(scope_overlay.go: ValidateForStorage on the overlay). The write is the other end of the same
rule and has never been held to it.

Baseline writes should NOT be held to it. `!arraydiff {0: 99}` on a two-element array is sound
in baseline and stays sound forever; refusing it would refuse a correct write to catch an
incorrect one. This is the distinction that was missing when the vocabulary was first proposed
as the fix for everything (trqgmd1ah12kranxg5n0):

    baseline   deterministic replay   ->  verify it applies once (done)
    scope      base moves             ->  hold it to the vocabulary

## Note

MergePatches manufactures an !arraydiff for a write at an array index, so the check goes on the
CLIENT's patch data before merging, not on the merged patch -- otherwise every positional write
in a scope is refused on account of logd's own routing.

Seen on main at 727faf5.