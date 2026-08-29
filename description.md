# logd: a narrow read restyles a value the wide read holds in flow, after a lowered relative write

ReadSubtreeAt's contract is that it answers exactly what the wide read holds at that
path. It does, in data and in comments. It does NOT in PRESENTATION.

## Shape

    wide    { k1: 2 k2: 4 }
    narrow  k1: 2 k2: 4

Same fields, same values, same comments, different style: the wide read holds the value
in flow and the narrow one in block. Every occurrence found so far follows a relative
write that lowering converted -- a !rename in these streams.

## Not scope-specific, and not new

Found by TestNarrowScopedReadMatchesTheWideRead once a scoped read could narrow, but the
BASELINE narrow read does it too, on the same generated streams and with no scope
involved: 45 divergences over 200 seeds, every one of them presentation-only. Comparing
with stripPresentationDeepIR on both sides gives 0.

So it is a property of narrowSubtreeAt rather than of the scope layer, and it predates
the scope being able to narrow at all -- it was simply not measured, because the only
narrow-vs-wide test ran over a handcrafted store rather than generated streams.

## Why it matters, and why it is not urgent

Presentation is how a value was WRITTEN, not what it is, and ir/tags.go names it a
category patching drops first. A client that reads a subtree and a client that reads the
document therefore see the same data rendered two ways, which is a real difference to
anybody diffing or hashing the text, and no difference at all to anybody reading values.

It is not a data or comment loss, and both reads agree on those.

## Where to look

narrowSubtreeAt applies patches projected to kp onto the snapshot's own subtree at kp
(snap.ReadPathEventReader), where the wide read applies whole patches onto the whole
snapshot stream. The two bases come out of the snapshot by different readers, so the
first suspect is what the path reader preserves versus what the whole-document stream
does, and the second is whether a projected patch carries the style tag its unprojected
form did.

## Reproducing

TestNarrowScopedReadMatchesTheWideRead counts them as `restyled` and does not fail on
them; LOGD_SEEDS=200 reports 4 on the relative stream and 0 on the plain one. The
baseline half was measured with a throwaway harness over the same generator, forcing
every op to baseline.