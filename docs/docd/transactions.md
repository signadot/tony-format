# Multi-mount transactions

A single client `Patch` can touch paths owned by different owners at once — some base
(logd), some under one mount, some under another. Each owner must write its own
subtree, yet the whole write must land **atomically**. docd achieves this by
decomposing the patch and applying the parts as one multi-participant transaction.

## Static decomposition

docd splits a client patch, by path, into:

- a **per-mount sub-patch** for each mount the patch touches, rooted at that mount's
  path and sent to its controller; and
- **base writes** for everything left over, applied by docd over its own logd link.

The split is **static** — computed from the patch's structure without reading current
state. Each base write sits at a path that is not an ancestor of any mount, so no two
participants' paths overlap and the transaction merge (which rejects a patch whose
path is a prefix of another's) is never triggered.

Some tags sitting on a node **above** a mount boundary would make a purely structural
split ambiguous — the tag's effect could depend on the mounted content docd does not
have. Such a patch is **rejected** rather than mis-split, keeping decomposition
correct-by-construction.

## Multi-participant transactions

logd transactions are counted by **participants**: a transaction id is allocated for a
known number of writers, and logd commits it only once every participant has written.
docd uses this to coordinate a spanning patch:

1. Allocate a transaction id for *N* participants (one per mount part plus base).
2. Hand each participant the id; each writes its part against it.
3. logd commits atomically when the last participant's write arrives.

If any participant fails, the transaction does not commit — the client sees a single
failure rather than a partial write.

## The transaction-id pool

Allocating a transaction id is a round trip to logd. To keep spanning writes cheap,
docd keeps a small **pool** of pre-fetched transaction ids
(package `system/docd/txpool`), replenished in the background and keyed by participant
count, so a multi-mount write usually draws its id locally instead of waiting on logd.

A single-owner write needs no transaction id at all — it is forwarded straight to its
owner.

## Scopes

A client's copy-on-write **scope**, set once in `Hello`, is threaded through every
operation so that reads, writes, and watches all see the same isolated view:

- **base** operations carry the scope to logd; and
- **controller** operations carry the scope *in the request*, so one multiplexed mount
  connection serves every client scope without a connection per scope.

Scoped writes go to the scope without touching baseline, and scoped reads see the
scope's data overlaid on baseline (copy-on-write). `DeleteScope` — dropping a scope and
all its data — is a baseline-only operation; a scoped session cannot delete scopes.
