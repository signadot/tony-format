# logd: a patch past the end of an array is accepted and makes the log unreadable

A patch at an array index one past the end is ACCEPTED and commits. Every read after it fails,
including reads of unrelated entities, and no client can undo it.

## Reproduce

Two elements stand at `verse.demo.pr.1.votes`; patch element 2.

```go
// seed: verse.demo.pr.1 = {votes: [{by: scott, choice: approve}, {by: dee, choice: reject}]}
res, err := sess.Patch(ctx, `verse.demo.pr."1".votes[2]`, node(`{by: ana}`))
// err == nil, res.Commit == 3   ← accepted
```

Then every read fails:

```
match error: storage_error: failed to read state: failed to apply patches:
  arraydiff patching "- by: scott\n  choice: approve\n- by: dee\n  choice: reject"
  gave invalid arraydiff at $.Patch.verse.demo.pr.1.votes:
  patch !logd-patch-root at key 2 reaches element 2 of 2
```

## What is lost

Not the entity — the LOG. Everything replays through that patch:

  - reading `demo:pr:1` fails
  - reading `demo:pr:2`, which nothing in this test ever touched, fails with the same error
  - the whole-root listing comes back with 0 entities

And it is unrepairable from a client. A fresh session replays the same log; removing the field
and removing the entity are patches appended AFTER the bad one, so the read still dies on the
way past it. All three checked.

## Where the check belongs

At the patch, not at the read. The write is the moment the array's length is known and the
moment a caller can be told; by read time the information is gone and so is the store. Refusing
it would be consistent with the keyed spelling, which already errors at commit and harms
nothing: `votes(scott).choice` answers `failed to merge patches: ir node unspecified`.

An in-range index patch (`votes[1].choice`) works correctly, which is what makes this sharp: the
same shape is fine one index to the left.

## Why this is being filed rather than worked around

verse refuses array element writes at its own boundary (`entity.ParsePath`), so it cannot emit
this patch. That refusal was there for a different, wrong reason — "the substrate patches at
object fields" — and this is the reason it now has. The three behaviours above are pinned in
verse's `entity/arraywrite_test.go`, including this one, so the day it starts being refused,
verse hears about it.

Seen on go-tony v0.0.147.