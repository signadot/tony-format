# logd: !logd-patch-root masks the client's op in an array-element write -- !insert silently overwrites, !delete panics every reader

logd tags every patch root with `!logd-patch-root` before merging. For a write at an array
index that tag lands on the exact node `patchArrayByIndex` dispatches on, so the client's op
is never seen: `!insert` silently overwrites, `!delete` panics every reader, and an append
poisons the log.

## The format is right; logd is what breaks it

Applying arraydiffs directly (`api.NextState`, nothing of logd in the way) on `v: [1, 2]`:

```
!arraydiff {1: !insert 99}   ->  [1, 99, 2]     insert
!arraydiff {2: !insert 99}   ->  [1, 2, 99]     append at len
!arraydiff {0: !insert 99}   ->  [99, 1, 2]
!arraydiff {1: 99}           ->  [1, 99]        patch in range
!arraydiff {0: !insert 99}   ->  [99]           on []
```

The same writes through logd, seeded with `{votes: [{by: scott}, {by: dee}]}`:

| write | should be | is |
|---|---|---|
| `votes[1]` `!insert {by: ana}` | `[scott, ana, dee]` | `[scott, ana]` -- **dee silently overwritten** |
| `votes[0]` `!insert {by: ana}` | `[ana, scott, dee]` | `[ana, dee]` -- silently overwritten |
| `votes[2]` `!insert {by: ana}` | `[scott, dee, ana]` | commits; **every read fails forever** |
| `votes[0]` `!delete ...` | `[dee]` | commits; **every reader panics** (nil deref, patch.go:278) |

## Mechanism

`TagPatchRoots` composes the tag onto the client's data root (`storage/tx/patch_root.go:23`),
called at `storage/tx/coord.go:383`, before `MergePatches` wraps a `votes[i]` path in an
`!arraydiff`. So the tagged node IS the arraydiff's op node. `patchArrayByIndex` dispatches on
the head of the tag chain (`mergeop/arraydiff.go:92`), which is now `!logd-patch-root`, so
every op falls to the default branch -- patch the element at this position -- which is
coincidentally right for plain in-range data and wrong for everything else.

It also writes the internal tag into stored user data:

```
{v: !arraydiff {1: !logd-patch-root.insert 99}}  ->  [1, !logd-patch-root 99]
```

## Why it stayed hidden

In range, with plain data, the default branch does the merge-patch the client meant, so the
masking is invisible. It only shows when the leaf carries an op, or when the index is past the
end -- which is the "same shape is fine one index to the left" observation in
7cdvym1fh12ksmd5g5n0.

## Relation to 7cdvym1fh12ksmd5g5n0

That issue reads as "a patch past the end of an array is accepted". Most of it is this bug:
fix the masking and append, insert and delete all come right with no state consulted. What is
left of 7cdv afterwards is the genuine existence check -- plain data at `votes[i]` where the
element does not exist -- which is a submit-time question, not a read-time one.

## Where the fix belongs

At the tag, not at the dispatch. Either keep the marker off a node whose tag is load-bearing
for the merge, or compose it so the client's op stays at the head and the marker is found by
`TagHas` (which `HasPatchRootTag` already uses) rather than by being first. Whichever way, the
stored value must not come back carrying it.

Seen on main at 30817c5.