# logd capabilities

**logd** is the append-only, versioned commit store beneath [docd](../docd/index.md).
Every write is a numbered *commit*; state is reconstructed by applying patches over
snapshots. A handful of capabilities fall out of that design — and because docd speaks
the logd protocol verbatim, clients get them through docd too.

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

docd inherits it for single-route watches. Across mount boundaries it is *best-effort*:
a composed watch spans backends with independent commit sequences, so a mount
membership change **re-initializes** the watch (a fresh composed snapshot) rather than
replaying the gap. See [Composition](../docd/composition.md) for the full contract.

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
