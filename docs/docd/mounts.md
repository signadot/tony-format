# Mounts & routing

A **mount** delegates a subtree of the document to a controller process. This page
covers how a controller registers a mount, how docd routes each client operation to
its single owner, and what happens when a controller goes away.

## The MOUNT protocol

A controller connects to docd's mount listener and performs a two-step handshake, both
steps as Tony documents:

1. **Hello** — `{hello: {controller: "<id>"}}`. docd replies with its own identity,
   `{hello: {docdId: "<id>"}}`.
2. **Mount** — `{mount: {path: "<path>", schema: <schema>, forceAfter: "<dur>"}}`.
   docd replies `{mount: {path: "<path>", accepted: true}}`.

After the mount is accepted, the controller owns that subtree: docd forwards every
client `Match`, `Patch`, and `Watch` whose path falls at or under `path` to this
controller, and relays the responses back. A controller may also **unmount**
(`{unmount: {forceAfter: "<dur>"}}`) to release the subtree.

`forceAfter` bounds how long the mount/unmount waits for overlapping watches to drain
before force-ending them — see [Composition](./composition.md#coordination).

## The mount registry

docd keeps a registry of `MountEntry` records — the mounted `path`, the owning
`controller`, its contributed `schema`, and the live `session`. Mounts are
**single-owner**: a path has exactly one owner, and controllers may not mount under
the reserved `.meta` namespace.

Mounts may **nest**: a controller can own `users` while another owns
`users.alice.inbox`. Routing always resolves an operation to the *deepest* mount that
covers its path.

## Routing

Every client operation is routed by path to exactly one destination:

| Destination | When |
|-------------|------|
| **logd** (base) | the path is outside every mount, or it is a session-level op |
| **controller** | the path is at or under a live mount |
| **docd itself** | the path is `.meta` or under it |
| **unavailable** | the path is under a mount whose controller has disconnected (a *tombstone*) |

An operation whose path is a strict *ancestor* of one or more mounts is not
single-routed — it is **composed** across the owners beneath it. See
[Composition](./composition.md).

## Tombstones

When a controller disconnects, docd does **not** delete its mount — it keeps the entry
as a **tombstone** (a mount record with no live session). Operations on that subtree
then fail with a clear *controller-unavailable* error.

This is deliberate. The mounted content lived *in the controller*, not in logd, so if
docd simply removed the mount, reads of that subtree would silently fall through to
logd and return stale or empty base data — a correctness trap. Failing loudly lets the
client distinguish "the owner is down, retry" from "this data does not exist." A
**remount** by the same (or a replacement) controller clears the tombstone and
restores service.

## The `.meta` namespace

docd serves a small, reserved `.meta` namespace itself — it is never routed to logd or
a controller, and controllers may not mount under it:

- **`.meta/mounts`** — the current set of mounts (path, controller, live/tombstoned).
- **`.meta/schema`** — the schema composed from every mount's contribution plus base.
- **`.meta`** — a self-describing index listing the resources above.

Because `.meta` is computed from docd's own registry, it reflects mount membership at
read time and is the canonical way for a client to discover the document's shape.
