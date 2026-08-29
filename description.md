# logd: a root-marked lowered delta leaves !logd-patch-root on the document, and the next root operation fails on the tag

Only reachable with LOGD_LOWERING=all today, which is the test amplifier and not a mode
to run in (see Storage.LowerEverything). Filed because it is a defect in what a lowered
delta promises -- that it can be re-applied -- rather than in the amplifier.

## Reduced

Four baseline writes at the ROOT. No scope, no snapshot, nothing keyed.

    baseline "" <- {k0: 0}
    baseline "" <- !rename [{from: "k0", to: "k0"}]
    baseline "" <- {k1: 4}
    baseline "" <- !delete

with LowerEverything(true). The last write is accepted, and then a plain BASELINE read
of the document fails outright:

    failed to apply patches: delete patching "!logd-patch-root\n{\n  k1: 4\n}"
    gave doc tag !bracket.logd-patch-root at $ didn't match bracket

## What is happening

markDeltaRoots marks a delta at its root when it cannot descend to a deeper container
(lower.go). Applied to an empty base, that marked delta becomes the base, and the marker
travels with it: the document being patched is `!logd-patch-root {k1: 4}`. The next
operation at the root asks the document for its tag, gets `!bracket.logd-patch-root`
where it expects `bracket`, and refuses.

So the marker -- logd's own bookkeeping, not the client's data and not an operation --
is visible to a merge operation as though it were part of the document.

## Why the default mode does not hit it

With lowering on but not forced, `{k1: 4}` is absolute and is kept as the client sent
it, marked by tx.TagPatchRoots rather than markDeltaRoots. The same four writes in the
default mode read back clean. It takes a LOWERED delta rooted at the document root,
followed by an operation at the root, and only the forced mode produces the first.

That bounds the severity, and it does not make the shape unreachable: any lowering
rule that ever roots a delta at the document root reaches it.

## How it was found

TestAScopedWriteIsAStandingClaim (system/logd/storage/lower_claim_diff_test.go), which
is no longer skipped, run as LOGD_SEEDS=500 LOGD_LOWERING=all: 2 broken claims in 6121,
seeds 149 and 272. Both reduce to the four ops above. At the documented soak --
LOGD_SEEDS=100 LOGD_LOWERING=all -- it does not fire, and neither does 500 seeds in the
default mode.