# logd: a narrow read silently returns STALE data when a write's patch root is above the read path

Severity: silent wrong answers on the default pathed read path. No error, no log
line, no fallback -- the read reports success and returns pre-snapshot data.

Pre-existing and shipped: reproduced identically on `main` and on
`worktree-lowering`. Found while analysing p83xdgm2h12kre9ajdn0, which is a
symptom of this and not a presentation issue.

## The rule

A narrow read at a path STRICTLY BELOW a post-snapshot write's patch-root marker
drops that write entirely and answers from the snapshot.

Measured (`seed = {a: {b: {k: 1}}, z: 0}` written, then the row's write, then the
row's read):

    write at root, snapshot, read a         LOST: narrow {b: {k: 1}} vs wide {b: {k: 99}}
    write at root, NO snapshot, read a      agree
    write at a,    snapshot, read a         agree
    write at a,    snapshot, read a.b       LOST: narrow {k: 1} vs wide {k: 99}
    write at a,    snapshot, read a.b.k     LOST: narrow 1 vs wide 99
    write at a.b,  snapshot, read a         agree
    write at a.b,  snapshot, read a.b.k     LOST: narrow 1 vs wide 99

So "write an entity, then read one of its fields" is the broken shape:

    write  a       <- {b: {k: 99}}
    read   a.b.k   -> 1, the value from before the last snapshot

Needs no scope, no lowering, no keyed array, no operator. A snapshot is required
-- with none, findSubtreeBaseReader returns an empty reader, ApplyPatches takes
its empty-base branch and folds every patch whole, where markers do not matter.

## Mechanism

A stored entry's `!logd-patch-root` says where the entry is applied FROM, and the
streaming processor finds what to apply by walking for it. `patchAtPath`
(read_subtree.go) re-roots a patch to the read path by DESCENDING into it -- and
descends straight past the marker. The projected node is then an unmarked patch,
the processor finds no root in it, and it contributes nothing.

    stored:     {a: !logd-patch-root {b: {k: 99}}}
    read a.b:   patchAtPath descends a, then b  ->  {k: 99}, no marker  ->  dropped

Reading AT the marker's path works, because the projection is the marked node
itself. Reading ABOVE it works, because the marker is inside the projected
subtree. Only below is broken.

## The fix, stated as an invariant

A marker says where an entry applies from. A projection to kp RE-ROOTS the entry,
so its marker has to be re-rooted too: if the patch carried a marker at or above
kp, the projected node is a patch root at kp and must be marked as one.
`patchAtPath` already walks exactly the segments that would have to be checked, so
it can report whether it passed one.

A marker BELOW kp is already inside the projection and needs nothing. Both
together is fine and is the designed case -- buildPatchValueIndex deliberately
keeps dominated roots.

## Why the differentials missed it

`TestReadSubtreeMatchesTheWideRead` writes deep (`verse.entities.eN`) and reads at
or above those paths; the two writes it makes after its snapshot are read at their
own path, never below it.

`TestNarrowScopedReadMatchesTheWideRead` generates root writes as `{k0: N}` --
fields at the root, never a subtree under a path it also reads -- so an
ancestor-marked write never states anything about a descendant it checks. That is
the coverage gap, and it is the same gap in both: no case where a write's marker
is strictly above a read path AND the write says something about that path.

## Reproducing

Read-only, outside any checkout: a module with a `replace` to the tree under test,
using only the public API (storage.Open, NewTx/NewPatcher/Commit, SwitchDLog,
ReadStateAt, ReadSubtreeAt). The seven rows above are that program's output, and
it prints the same on main.