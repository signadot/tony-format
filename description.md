# mergeop: !nullify rewrites the document it is given, so logd stores it unlowered and its watchers never see the commit

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53, in the
storage API and on a live server.

mergeop/nullify.go:40-42 rewrites the node it is given: after
tony.Patch(doc, {spec: !nullify null}), doc.spec itself is null.

In logd: lowerWrite (storage/lower.go:189) folds the write onto its base, so base and next
are one object and their diff is nil (:234); commit_ops.go:75/98 then keeps the patch as
sent, skipping ValidateForStorage. Writing {spec: {a: 1}, k: 1} and then
{spec: !nullify null} stores `@2 {spec: !nullify null}`, an operation StorageContext
excludes. Live, with watches on "" and on spec, commit 2 produces NO event on either --
the next is commit 3: applyAt (server/session_watch.go:371) folds the stored !nullify
into w.prev in place, so SameState(next, w.prev) (:391) holds.

Consequence: event loss on a live watch, and an unstorable operation in the log. Fresh
reads are right (state at c1 is intact after c2); the snapshot builder was not probed.

It breaks mergeop/field.go:64's contract, "a patch never mutates what it is given"; the
related issue is the same contract broken by sharing.