# docd-coordinated patch splitting (transparent multi-mount transactions)

This is the intended purpose of docd transactions, and a core reason mountable
controllers exist: **a client just sends a patch, and docd makes it atomic across
whatever mounts it touches.**

## The intent (scott)

docd transactions are NOT meant to be driven explicitly by clients. A client does
not usually need transactions in its hands — it "just updates what it wants under
what conditions in a patch and that just works." docd's transactions are not
intended for anything more than that: making a single client update that spans
multiple mountable controllers commit atomically.

The explicit NewTx + N PatchTx capability that exists today (LogdSession.NewTx /
PatchTx, and docd serving NewTx from the pool) is the lower-level plumbing. The
primary, intended model is transparent splitting described here — clients should
rarely, if ever, touch NewTx directly.

## What to build

A client sends ONE patch: a path, its match/preconditions, and the data to write.
That patch may span multiple mounted subtrees. docd:

1. Determines which mounts the patch touches (the participating controllers).
2. If it touches a single mount (or only base/logd), route as today — no tx.
3. If it spans N mounts, allocate a pooled TxID with participant count N.
4. Split the patch into per-mount sub-patches (preserving each subtree's
   match/preconditions).
5. Send each controller its sub-patch carrying the TxID.
6. Controllers join by writing to logd with the TxID (write = join, WOL).
7. logd commits atomically when all participants have written; docd returns one
   result to the client.

Match/precondition semantics must be preserved per participant so the whole
update is all-or-nothing under the stated conditions.

## Open questions

- Patch shape: how a client expresses a multi-mount patch (root path with
  per-mount subtrees in the data vs. an explicit multi-path form).
- Splitting a diff/patch document by mount subtree (top-level vs. nested mounts).
- Partial failure: one controller's precondition fails or it errors before
  writing -> abort the tx; return a coherent error to the client.
- Controller-crash mid-tx -> deterministic "outcome unknown" to the client.
- Interaction with scoped clients (see the scoped multi-mount tx issue).

## Relation

Blocks the docd umbrella (wcabztj2h12ksb9qbnn0): this is the transaction model the
umbrella is really after; the current explicit NewTx/PatchTx path is groundwork.