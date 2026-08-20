# logd: a watch on a quiet path ages out of its own resume window

Scott's point, from the identical-write investigation (xmxt2p85h12ksjp1gsn0): suppressing an
event is right for the consumer and wrong for the cursor, and compaction is what makes it
bite.

THE MECHANISM

  - A commit which does not change the watched subtree sends nothing: stepBaseline folds it
    and api.SameState gates it. That is the property a consumer's idempotence rests on and
    it should stay.
  - A client's resume point is whatever commit it last SAW. On a quiet path that number
    stops moving while the store keeps committing.
  - Compaction raises the replay floor (raiseReplayFloor), and a watch whose fromCommit is
    below it is refused with replay_compacted -- correctly, because the exact history it
    asked for is gone.
  - So a client which is perfectly up to date, and has been told nothing because nothing
    changed, is refused its resume and must re-initialize: a full read of the watched
    subtree, which for a large set is the expensive thing the resume existed to avoid.

The server already knows the right answer. forwardEvents tracks lastDelivered -- the commit
the watch is CORRECT THROUGH, advanced even for commits it suppressed or filtered -- and
hands it to the client on a terminal event as the resume point. It is never sent while the
watch is healthy, which is exactly when it would be useful.

WHAT WOULD FIX IT

  A. A watermark event: a WatchEvent carrying a commit and neither state nor patch, meaning
     "you are current through C". It costs one small message, it needs no new machinery
     (lastDelivered is already the number), and a consumer that ignores unknown event shapes
     ignores it safely. The question is WHEN: one per suppressed commit defeats the
     suppression, so a rate -- at most one per interval, or when the gap between lastDelivered
     and the last delivered EVENT exceeds some commits -- and the rate wants to relate to how
     fast the floor moves, which is compaction's business.

  B. Re-init instead of refusing when a cursor is below the floor. Simple, and it is already
     what a RELATIVE cursor does (-N clamps to the floor, v0.0.176). It gives up the
     exactness contract for absolute cursors, which is the thing that makes them worth
     having.

  C. Nothing in the protocol: a client can already see the floor. PongResult carries Commit
     and Floor (v0.0.184), so a heartbeat says both where the store is and how far back it
     can replay. A client whose cursor approaches the floor can re-watch BEFORE it is
     refused -- paying the re-init at a time of its choosing rather than at a reconnect.

C works today and is worth doing on the client side whatever else happens; A is the honest
fix; B is the one to avoid, since it quietly turns an exact resume into an approximate one.

ACROSS DOCD. A composed watch multiplexes sub-streams, so a watermark from one of them is not
the composed watch's watermark: it is current through the MINIMUM of its sub-streams'. That is
computable -- docd already sees every event -- but it is not free, and it is the reason to
decide A's shape before writing it.

WORTH MEASURING FIRST. How fast does the floor actually move relative to a quiet watch? On a
store committing steadily with compaction configured, the interval between "cursor stops
moving" and "cursor below floor" is the whole size of the problem, and it may be hours. If it
is, C alone is enough and A is not worth the wire.