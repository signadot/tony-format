# logd: a scope's delete of a field baseline never had is lost when the overlay is written

A scope's deletion of a field baseline has not written is lost when an overlay is
materialized, so baseline's later write shows through into a scope that had
removed it.

Three ops, reduced from seed 22 of the claim differential:

    scope     d <- {k0: !delete}
    scope     d <- {k1: 20}        + snapshot   (writes the overlay)
    baseline  d <- {k0: 26}

    scope reads   d: {k0: 26, k1: 20}
    should read   d: {k1: 20}

It needs the overlay and it needs the snapshot, and it does not need lowering:

    lowering=false overlay=false snapshot=false   d.k0 = <nil>
    lowering=false overlay=false snapshot=true    d.k0 = <nil>
    lowering=false overlay=true  snapshot=false   d.k0 = <nil>
    lowering=false overlay=true  snapshot=true    d.k0 = 26     <--
    lowering=true  overlay=true  snapshot=true    d.k0 = 26     <--

The cause is an assumption written at the place it fails, scope_overlay.go:114,
in the owned-path half of BuildScopeOverlay:

    v, err := scoped.GetKPathWith(p, ir.WithComments(true))
    if err != nil || v == nil {
        continue // absent in the scoped view: the diff's tombstone is what holds it
    }

There is no tombstone. The overlay's other half is Diff(baseline@T, scoped@T),
and at T baseline has no d.k0 either -- the scope deleted something baseline had
not yet created -- so the diff has nothing to say about it. "k0 is not here" is
not a DIFFERENCE between the two states, and the only thing that recorded it was
the scope's own patch, which the overlay is standing in for.

The same shape one layer down is what the scope-claim work on
4wpqh7t2h12ks1fvj5n0 is about: `a.b <- !delete` where a.b does not exist yet
diffs to `a: {}`, which merges to nothing, and the claim is gone. claimDelta
answers it by stating absence AS a claim --

    rooted, err = tx.RootPatchAt(p, ir.Null().WithTag(libdiff.DeleteTag))

-- and the owned-path loop wants the mirror of that where it currently
continues: a path the scope owns and does not hold is owned as absent, and an
overlay re-stating what the scope holds has to re-state that too.

Worth looking at in the same pass: scopeOwnedLeafPaths keeps only the deepest
path of any chain, so a delete that removed a whole subtree leaves index paths
for nodes that are no longer there, and the same continue swallows every one of
them.