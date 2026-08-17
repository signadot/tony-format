# logd: a read ignores its path — every read is a whole-document read, because the index duplicates an entry per level

ReadStateAt takes a kpath and never uses it. Every read replays the whole base plus
every delta and materializes the whole document, whatever path was asked for. A
caller reading one entity out of a tree pays for the tree.

MEASURED. A store holding 200 entities of ~2 KB each under separate top-level keys,
plus one small node off to the side, read at commit head:

    read ""                     404622 bytes   12.5 ms
    read "verse"                404622 bytes   12.8 ms
    read "verse.entities"       404622 bytes   12.3 ms
    read "verse.entities.e7"    404622 bytes   13.0 ms
    read "verse.meta.rev"       404622 bytes   13.1 ms
    read "nope.not.here"        404622 bytes   13.2 ms

Identical bytes, identical time, including for a path the document does not have.
The cost splits:

    replay + materialize   12.0 ms   O(base + deltas)   <- what a path should reduce
    encode                  2.9 ms   O(payload)         <- what a projection reduces

WHY, AND IT IS DELIBERATE. storage.go:236 says so: narrowing was tried and came out
~5x SLOWER. index.indexPatchRec indexes each entry at every ancestor path (it starts
at ""), so LookupRange(kp) returns the root's complete set PLUS a repeat of each entry
for every level of kp, and the applier applied each entry once per repeat -- four
times for a three-segment path. Reading at the root was the way around it.

So the thing in the way is not that narrow reads are hard. It is that the index
answers a path query with duplicates, and the root read is a workaround for that.

WHAT A NARROW READ NEEDS, two independent pieces:

  1. Delta side: the entries touching a subtree, each ONCE. Either dedup in
     LookupRange, or index an entry at one path and let an ancestor query widen,
     rather than the index widening at write time. This is the half that is a bug
     with a workaround rather than a missing capability.

  2. Base side: reading only the subtree out of the snapshot, which is what makes a
     narrow read O(subtree) rather than O(state). Not unexplored -- internal/snap
     plus docs/snapshot_path_indexing_options.md,
     docs/snapshot_index_kpath_structure.md and
     docs/snapshot_index_range_descriptors.md are this question.

WHO PAYS TODAY. Anything reading a part of a large tree: a status endpoint reading a
count (455 KB per probe, measured on a staging verse), a controller reading its own
subtree, a client resolving one entity by id. It is also why narrowing was no help to
the readiness probe in g02yc3r4h12ks8ksgdn0, and why dp5y7ahhh12ksgbvgdn0 (no cheap
revision query) has no cheap answer: there is no cheap read of any kind.

A projection (!fields, filed separately) buys the 2.9 ms and the wire; this issue is
the 12 ms.