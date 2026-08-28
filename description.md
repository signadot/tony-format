# logd: a root-level delete cannot be lowered, because a diff of two states cannot say absent

Running the logd suite with every write lowered (`LOGD_LOWERING=all`, see
`Storage.LowerEverything`) leaves two failures, and they are one class: a write that
deletes at the ROOT.

```
head_test.go:72: seed 1 op 11 "" <- !delete: head diverged at commit 12
     head: <nil>
     want: null
head_test.go:83: seed 3 op 11: the snapshot check dropped the head at commit 12
```

`TestHeadEquivalence_SteppedVsReference` and `TestReadEquivalence_SnapshotVsReference`
are the two, and both are equivalence checks between the stepped head and a full read.

## what it is

`verifyApplies` answers with the state the write produced. When the write removes
everything, the stepped head holds ABSENCE and a lowered delta stored from a diff of
two states can only say "the document is null" -- the absent document is not the null
one, and a diff has no third answer.

`lowerWrite` has a branch for `next == nil` that stores a root `!delete` instead, and
it does not fire, so `api.NextState` is answering with null rather than nil for a root
delete. Which of those is right is the question this needs answered before the branch
can be written correctly.

## why it is not a blocker for the ordinary rule

Under `NeedsLowering` -- the rule that ships -- a `!delete` is ABSOLUTE and is stored
as it arrived, so this path is not reached. It shows up only when every write is
forced through the lowering, which is a test mode.

It still has to be settled: `LowerEverything` is the only differential that says
anything, because with the ordinary rule the suite exercises lowering 16 times in
21587 writes.

## the ones already fixed, for the record

The same differential started at 11 failures. Four causes, all now closed:

1. keyed arrays came out positional -- `lowerWrite` did not annotate from the schema
   the way `BuildScopeOverlay` does, so a scoped write froze the whole array.
2. an UNDECLARED key cannot survive lowering at all -- the key rides in the client's
   patch and lowering replaces the patch. Such a write is now not lowered, the same
   conservative answer `scopeHasKeyedPaths` gives the overlay.
3. every narrow read replayed every write -- `patches.BuildPatchIndex` keys entries by
   the PATH of each `!logd-patch-root`, and marking the delta's root made every entry
   a patch on the whole document. `markDeltaRoots` marks where the change lands.
4. an introduced subtree claimed its parent -- `!insert` replaces (5k4a6drqh12ksnsaj5n0),
   so a scope writing `{a: {x: 5}}` into an empty document said "a is {x: 5}" and wiped
   a later `a.y`. Absoluteness is a property of each path a delta NAMES, not of the
   subtree it hangs under, so an introduced object states its own fields.