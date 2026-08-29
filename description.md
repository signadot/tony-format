# logd: a scoped read misses the write that follows an overlay -- the overlay path disagrees with the replay it optimises

readScopedStateAtOverlay disagrees with readScopedStateAtReplay -- the definition
it is an optimisation of, and which its own comment names as the oracle it is
checked against.

The scope write immediately following an overlay is missing from a scoped read at
that write's own commit. It appears once any later commit lands.

    scope     a   <- !delete
    baseline  a   <- {k1: 1}
    scope     d   <- {k1: 2}      + snapshot   (writes overlay)
    scope     d   <- {k0: 3}      + snapshot   (writes overlay)
    scope     d.e <- {k1: 5}      <- commit 5

    replay  at 5:  d: e: k1: 5 k0: 3 k1: 2      the definition
    overlay at 5:  d: k0: 3 k1: 2               the optimisation

Nothing about the write matters -- {k2: 5} at d, {k1: 5} at d, {k1: 5} at q, a
delete at d.e all vanish the same way, and all reappear on the next read. Nothing
about lowering matters either: identical with EnableLowering(true) and (false),
and the entry is in the log, correct and marked:

    scope @6: d: e: !logd-patch-root { k1: 5 }

Where it goes missing is the lookup, not the fold. storage.go:360

    after := ov.EndCommit
    for _, seg := range s.index.LookupRange("", &after, &commit, scopeID) {

with the overlay at EndCommit 4 and the write's segment at [4,5], that call
returns ONLY the overlay's own [3,4] segment. The write is not among them, and
the two filters below the call never see it. The unbounded call at the same
moment does return it:

    LookupRange("", nil,     &5, &scope)  ->  [0,1] [2,3]ov [2,3] [3,4]ov [3,4] [4,5]
    LookupRange("", &4,      &5, &scope)  ->  [3,4]ov

and index.rangeFunc accepts it on its face: the predicate keeps a segment whose
EndCommit is in [from, to], and 5 is in [4, 5].

So the bounded walk drops a segment its own predicate accepts. Two things in
that walk are worth looking at together, since both require the predicate to
agree with the order the segments are stored in:

  index/index.go:592  rangeFunc keys on EndCommit alone
  index/index.go:532  LogSegCompare orders by StartCommit, then StartTx, then
                      EndCommit -- and overlays carry a sentinel StartTx
                      (scopeOverlayTx), so segments with equal commits interleave
                      by a field the predicate does not know about
  index/node.go:288   leafRange binary-searches with sort.Find and stops at the
                      first element the predicate rejects
  index/node.go:306   parentRange prunes a child by r(c.max()) and r(c.min())

It is not scope-only in principle: replayBaselineAt and readScopedStateAtReplay
both call LookupRange with a from bound taken from a snapshot boundary. What
makes the scope case fire is the overlay's sentinel tx sitting between segments
with adjacent commits.

Found by the claim-stability differential on 4wpqh7t2h12ks1fvj5n0 (seeds 5 and
14 there); reduced to the above, which needs neither lowering nor a scope claim.