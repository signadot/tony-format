# logd: a replaying watch delivers the stored patch, internal markers and all

Reported from verse: identical rewrites raise a change, and a delete arrives as a set --
both only on a watch that RESUMES from a commit. Reproduced, and it is one cause.

    LIVE:   delta at 2 = {verse: {items: {b: {n: 2}}}}
    LIVE:   delta at 4 = {verse: {items: {a: !delete {n: 1}}}}

    REPLAY: delta at 2 = {verse: {items: {b: !logd-patch-root {n: 2}}}}
    REPLAY: delta at 3 = {verse: {items: {b: !logd-patch-root {n: 2}}}}
    REPLAY: delta at 4 = {verse: {items: {a: !delete.logd-patch-root {n: 1}}}}

!logd-patch-root is an internal marker: tx.TagPatchRoots puts it on each patch's root before
merging, and the read path uses it to find which subtrees a commit patched. It is
deliberately stored. What it must never do is leave the store, and the live path sees to
that -- newCommitNotification takes a deep copy and strips it. ReadPatchesInRange, which is
what a replaying watch reads, built its notifications straight from entry.Patch.

So a resumed watch got a different delta from a live one for the same commit, and that
produced both symptoms:

  - a consumer testing n.Tag == "!delete" sees "!delete.logd-patch-root" and reads a
    deletion as an ordinary write;
  - the extra tag makes the folded state differ from the state before it, so the change gate
    that suppresses an identical write (api.SameState) stops suppressing, and every rewrite
    on a resumed watch looks like a change. Commit 3 above is exactly that: live suppressed
    it, replay did not.

Fixed by giving the client-facing form one owner -- storage.DeliverablePatch, copy and
strip -- called by both paths. They were two copies of one rule and they drifted, which is
the third time this month (mount arithmetic in docd, and the read family in storage).

Not a regression from any recent release: the replay path has always done this. What
changed is that verse started RESUMING watches, which the relative cursor (-N, v0.0.176) and
composed-watch replay (v0.0.177) made worth doing. It was reported against v0.0.179 ->
v0.0.184 because that is when the resume path came into use.

TestLiveAndReplayDeliverTheSameDeltas holds them to being identical, including the
suppression, which is the property a consumer's idempotence rests on.