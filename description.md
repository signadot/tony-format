# logd: verifying a scoped write costs a scoped materialization -- a scope needs the stepped head baseline has

Every write is now applied to current state before it is stored, so a delta the store cannot
apply is refused instead of making every later read fail. For baseline that costs nothing: the
apply was already happening in `stepHead`, one step later, and the verified result IS the next
head. For a scope it costs a scoped materialization, and there is no scoped head to serve it
from (9b2vpggxh12ks0qde5n0).

## Measured

Unconditional writes, cost of the Nth write as the log grows, no overlay written:

```
baseline   first=404µs   @50=347µs    @200=507µs    @400=406µs     flat
scoped     first=466µs   @50=1.6ms    @200=7.9ms    @400=22.6ms    O(scope patches)
```

Where overlays ARE being written it is about what a scoped CAS write already pays -- ~850µs
flat (scope_overlay_costs_test.go). The bad case is a burst of scoped writes with no overlay
yet: `scopedHeadStateAt` falls back to a full scoped read, per write.

Accepted deliberately for now: correctness everywhere first, cost second.

## Shape of a fix

A stepped head per scope, which is what baseline has. The reason baseline's trick does not
transfer is that a scope's writes apply LAST and shadow baseline stickily, so folding a
BASELINE patch into a materialized scoped document lets baseline overwrite a leaf the scope
owns -- that is 9b2vpggxh12ks0qde5n0.

Stepping it with the scope's OWN patch does not have that problem, and this is the case the
commit path needs. The scoped view at C-1 is fold(baseline<=C-1) then fold(scope<=C-1); the new
scope patch at C applies last, which is exactly where it belongs. The condition is that the
cached view is at exactly C-1 -- and the commit lock is held, so the only commit since is this
one. A baseline commit in between invalidates it, and then it is recomputed.

So: keep a per-scope document with the commit it is current at, step it on a scoped commit,
drop it when the scope's view is not exactly one commit behind. Bursts of scoped writes go
flat; interleaved baseline traffic falls back to what it costs today.

Seen on main after the verify-before-store change.