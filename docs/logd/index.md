# logd capabilities

**logd** is the append-only, versioned commit store beneath [docd](../docd/index.md).
Every write is a numbered *commit*; state is reconstructed by applying patches over
snapshots. A handful of capabilities fall out of that design — and because docd speaks
the logd protocol verbatim, clients get them through docd too.

The wire itself — every request and response, what a watch promises, and the mount
protocol a controller speaks — is [The session protocol](session.md).

It also allows more than one view of the same store at once: a *scope* is an isolated
layer over the shared baseline, described below.

The same design decides what a store will accept, and what it keeps: a delta it cannot
apply would break every later read, so writes are checked before they are stored — and an
operation whose meaning depends on what it lands on is stored as the result it produced,
so that replaying it later cannot mean something else. See
[What a write must be](writes.md), and [Keyed arrays](keyed.md) for naming elements of
an array by identity rather than by position.

## Time travel

Every write produces a monotonic commit, and logd can reconstruct the document's state
at any commit — not just the latest. The watch API exposes this directly: a watch with
`fromCommit` set **replays the exact delta history** from that commit up to now and
then streams live, so a client that knows the last commit it saw can reconnect and
recover every change in between with no gap. History is *addressable*, not just
*current*.

How far back that replay stays exact is governed by [compaction](#compaction).

## Event preservation

A watch delivers **every state change as a discrete delta**, not merely a "something
changed" nudge — so an event-driven consumer sees each transition, in order. This is a
logd guarantee, resting on its single commit sequence.

docd inherits it for single-route watches. Across mount boundaries a change of
**membership** re-initializes the watch with a fresh composed snapshot rather than
replaying the gap — the composition changed, so deltas from before it describe a
different document. See [Composition](../docd/composition.md) for the full contract.

## Scopes

A **scope** is a copy-on-write layer over the store's shared *baseline*. A session
selects one in its `hello`, and from then on reads see baseline with that scope's own
writes applied on top, and writes land in the scope rather than in baseline. Nothing a
scope writes is visible to baseline or to any other scope.

A scope is not a branch, and not a snapshot. Baseline keeps moving underneath it, and the
scope keeps seeing those changes, except where it has written something itself:

```tony
# baseline holds {a: {x: 1, y: 2}}
# the scope writes  {a: {x: 5}}
# baseline then writes {a: {x: 99, y: 7}}

baseline reads  {a: {x: 99, y: 7}}
the scope reads {a: {x: 5,  y: 7}}
```

The scope keeps its own `x` and picks up baseline's new `y`. A scope's write shadows
later baseline writes at that path, and only at that path, which is what makes a scope
useful for trying a change against a store that other clients are still writing to.

That stickiness is why a scope stores something slightly different from what baseline
stores. Baseline records the difference a write made; a scope records the claim it made,
so that the claim still means what it meant however baseline moves afterwards. See
[What a write must be](writes.md#what-is-stored-is-the-result-a-write-produced).

A scope lives until it is deleted. `{deleteScope: {scopeId: <id>}}` removes it and all
its data, and is accepted only from a baseline session — a session inside a scope cannot
delete the scope it is reading through. Until then the scope's records are kept whatever
[compaction](#compaction) would otherwise drop, because for a scope the records are the
state: baseline snapshots do not contain them, so there is nothing coarser to fall back
on.

docd carries scopes across its routes. A controller serves many clients over one
connection, so the scope travels on each routed request rather than being fixed by the
controller's own `hello`; see [The session protocol](session.md#the-mount-protocol).

## Compaction

Append-only does not mean unbounded. logd compacts its log on a **logarithmic
retention** schedule:

- within a recent **cutoff**, every patch is kept, so reads and replays in that window
  are exact to the commit; and
- beyond the cutoff, history degrades gracefully to **snapshot granularity**, with
  snapshots kept in tiers whose intervals grow by a fixed multiplier — recent-ish
  history stays fairly dense, and the deep past thins out.

The result is a bounded store that still retains an accessible long tail of history,
rather than either unbounded growth or a hard truncation.

Compaction is **off unless a config file asks for it**, and it runs as the last step
of taking a snapshot rather than on a schedule of its own — so a store that stops
taking writes never compacts. See [Configuring compaction](compaction.md) for the
knobs and what cannot be asked of them.

## Conditions on writes

A patch can carry a **compare-and-swap precondition**: alongside the write, the client
supplies an expected value at a path, and the patch commits **only if** the current
state at that path still matches. On a mismatch the write is rejected (`match_failed`)
and nothing is committed — optimistic concurrency without locks.

Preconditions compose atomically across logd's multi-participant transactions (see
[Multi-mount transactions](../docd/transactions.md)). When the transaction is ready,
logd checks *every* participant's precondition against current state, and either they
all hold and the whole transaction commits, or one fails and the entire transaction
aborts — nothing is written. There is no partial commit.
