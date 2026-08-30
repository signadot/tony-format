# logd: a scoped write rebuilds its view whenever baseline commits between two of its own

A scope keeps its own document and steps it, so a RUN of scoped writes is flat (727faf5). The
condition for using the kept document is that it is at exactly C-1, and that is self-checking:
commits are numbered globally, so anything else committing leaves this scope's document a commit
behind. Interleaved baseline traffic is therefore what still pays — and it is the shape a real
deployment has, since baseline writes do not stop while a sandbox writes.

## Measured

On main at 39e7e70, one commit past the go-tony/v0.0.203 a consumer is pinning. `TestScaling_Writes`
in `system/logd/storage` covers the first two rows; the third is the attached
`scope_interleave_test.go`, which has no test today.

    write, cost of the Nth                    N=50     N=100    N=200    N=400
      baseline                                583µs    372µs    389µs    349µs
      scoped, nothing else committing         359µs    369µs    367µs    371µs    flat
      scoped, a baseline commit between      ~2.0ms   ~2.35ms  ~4.05ms  ~7.65ms   O(scope patches)

The third row is the measured pair — one baseline commit, one scoped — less the baseline write's
own ~360µs. It is about 9x better than when this issue opened (546µs / 3.1ms / 21.5ms / 71.0ms on
the same axis), and it is still 20x a scoped write with the store to itself, still growing with
the scope's write count.

## The first answer cannot be built any more

This issue proposed keeping the scope's OWN overlay rather than the folded view and stepping
that — `overlay' = apply(overlay, patch)`, with the scoped view as `apply(baselineHead, overlay)`
— so that the cost stops caring what baseline is doing. It said that overlapped
5hmq80f3h12krh1mbsn0 (bounded op-preserving scope overlay) and was worth doing as one piece.

Neither holds now. 73e2637 DELETED the scope overlay — "a cache of a layer nothing can derive" —
and 39e7e70 finished the prose that still described it. There is no overlay to keep, to step or
to bound, so that plan and its overlap are both gone, and anyone picking this up from the old
text would start from a design that was removed.

## What is still wanted

The property is unchanged: cost per scoped write proportional to the scope's OWN footprint,
rather than to the number of commits since its document was last current. Where that lives now
the overlay does not is the open question — whether the kept document can be advanced over the
baseline patch it is behind, which is what 9b2vpggxh12ks0qde5n0 says it cannot (a scope's writes
apply last and shadow baseline stickily, so folding a baseline patch into a materialized scoped
document lets baseline overwrite a leaf the scope owns), or whether the scope layer as it now
stands (30c5fa4) composes more cheaply than the fold does.

Deferred deliberately at first — correctness everywhere first, cost second. That deferral is
discharged: the correctness series landed, and this is the cost half that did not.

A consumer that federates is in the interleaved case whenever anybody is connected: a scope gets
its own store and its own writer while baseline traffic carries on. That is the deployment shape
these numbers are about, and it is why the flat row above is not the one to read.
