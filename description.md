# logd/docd: what a review pass found and did not fix

A review pass over system/logd and system/docd. What was fixable in place is fixed (see the
commit); this is what is structural, and what it would take.

FIXED IN PASSING, recorded so the shapes are known:

  - a SCOPED watch never advanced its resume point while streaming live, so a watch dropped
    after an hour handed the client the commit it started at, and the re-watch either
    replayed the hour or was refused as replay_compacted. The pre-filter skip did not
    advance it either.
  - a collected route to a controller was removed only when a response ARRIVED. Every caller
    has a timeout; the ones that gave up left the entry behind for the life of the mount
    session, one per timed-out read or participant, on exactly the controller already
    unwell.
  - storage_error (11 sites) and match_error had no constants, so no client could branch on
    them and the error table did not list them.
  - three Storage getters and one DLog helper nothing called.
  - schema get/set, migration complete/abort, and hello's usePending were on the wire,
    implemented by the server, documented -- and unreachable from libctl. Advertising an
    operation nothing can invoke is the same as not having it.

STRUCTURAL, NOT FIXED

1. The read paths in storage are a family of nine with overlapping jobs: ReadStateAt,
   readBaselineStateAt, readScopedStateAt, ReadSubtreeAt, ReadSubtreeRootedAt,
   readSubtreeNarrow, headStateAt, scopedHeadStateAt, stateForCommit. Each exists for a good
   reason and no one of them is wrong; together they are the hardest thing in the package to
   hold in mind, and three of this month's bugs were about which one a caller reached and
   what it cost. Worth one pass that names the axes (baseline vs scope, whole vs subtree,
   stepped vs replayed) and makes the set follow from them.

2. forwardEvents is 330 lines and holds four jobs: the initial state, the replay, the live
   loop, and the scoped/baseline split running through all three. Every bug in it this month
   was in the interaction between those, not within one. Splitting it is not cosmetic --
   lastDelivered above was missed precisely because the same bookkeeping is repeated four
   times in one function.

3. patches/applier.go still carries "TODO: Replace with streaming implementation that never
   materializes full document", and the storage CLAUDE.md says containers MUST be treated
   out-of-memory for subpaths. The whole-document fold is what makes a write O(document)
   even now that the merge is cheaper.

4. docd's composed watch and composed read each re-derive the mount set and the path
   arithmetic in their own way (composeCheck, splitPatch/partition, pathFields). They agree
   today because the same person wrote them in the same week. One of them -- the trim added
   for duplicate deltas -- had to reach into the write side's partition to stay honest,
   which is the shape of a seam that wants a single owner.

5. The session protocol has no version. logd, docd and libctl deploy together today and that
   is what makes flat-shape changes safe (k0d4y1m6h12kr7cdgdn0); nothing enforces it, and
   nothing tells a mismatched pair apart from a working one. A hello that carried a protocol
   version, and refused what it cannot speak, is a day's work now and a bad afternoon later.

Also still open from this month: k0d4y1m6h12kr7cdgdn0 (an unknown request field is ignored,
so a misspelled path is the root).