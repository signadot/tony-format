# a one-path write costs the whole set: the fold rebuilds every field

Staging: writes failing with "context deadline exceeded" on arbitrary refs, from a
sensor reconciling a large set through docd (mountless, one small write per path) with a
few dozen watches over the same set. Scott: "it didn't used to."

It didn't. Bisected with a fixed benchmark -- seed N entities, then time 100 one-field
writes, median:

    tag                200 ent   1000 ent   3000 ent
    v0.0.120            522µs      513µs      548µs     flat
    v0.0.145            535µs      528µs      544µs     flat
    v0.0.150            526µs      744µs     1346µs     scaling
    v0.0.160            576µs      712µs     1186µs     scaling

Between 145 and 150, and then across the commit itself:

    8e1b334^ (before)   488µs      529µs      539µs
    8e1b334  (after)    560µs      726µs     1180µs

That is "logd: verify a patch applies before storing it, not after". Before it, an
unconditional write never materialized state; after it, EVERY write folds its patch onto
the whole baseline document (verifyApplies -> api.NextState). The commit is right --
storing a delta the store cannot apply is worse -- but the fold it added is not O(patch).

Why the fold is not O(patch): objPatchYWith merged two objects through a map. Every field
of the document went into it, the keys were sorted, and ir.FromMap allocated a fresh key
node for each. So patching one field of `verse.entities` rebuilt all N of them. Measured
on a 3000-field object:

    map + sort + fresh key nodes   557µs      <- what it did
    slices, fresh key nodes        149µs      <- what it does now
    slices, shared key nodes         9µs      <- what it could do

Fixed with objMergeFast: both sides are already in key order, so they are merged by
walking them, and only what changed is rebuilt. The general path stays for everything
else -- unsorted documents, !merge fields, keyed arrays. NextState over 3000 entities:
571µs -> 207µs; over 9000: 1.88ms -> 583µs. That fold is also what every watcher runs per
commit and what the store's head steps with, so dozens of watches over one set pay it
too.

WHAT IS LEFT. With the fold fixed, the leading term in a commit is now the index:

    3000 entities: total 343µs = apply 104µs + index 218µs + append 19µs

and index cost grows the same way. IndexPatch inserts a segment at every level of the
patch, into trees whose size grows with the commits retained at that level -- the root
tree gets one per write. Worth its own measurement before its own fix; the counters
(writes.avg.index) now report it from any running store.

Also landed, since the last round of this was diagnosed from the outside:

  - storage.WriteStats: commits, slow commits, head hits/misses, and per-phase averages
    (apply, append incl. fsync, index). A HEAD MISS is a write that read the whole
    document to find out what it was patching -- one after a restart is the cost of
    starting, a stream of them is a store paying a full read per write.
  - a "slow commit" WARN line over 250ms naming the path and the phase, plus O_DEBUG_WRITE
    for one line per commit.
  - WatchHub.Report: paths, watchers, broadcasts, delivered, dropped, and average fan-out
    time -- a dropped watch is a client re-establishing, which re-seeds with a read of the
    whole watched subtree.
  - the admin listener now reports reads AND writes AND watches (Storage.StatsReport).

Related: dvgz9308h12ks4xmgdn0 (the snapshot on the write path), 7qayp3hah12kscx2gdn0
(reads off the request loop), ap8ddvp2h12krd43gdn0 (read cost).