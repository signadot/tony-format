---
title: "A document store built out of a log: events, diffs, and two files"
published: false
status: draft
description: "logd stores documents as indexed event streams, applies writes as streaming diffs, and backs snapshots with two alternating log files. How that works, and how it compares to etcd and JSON-in-a-column."
tags: databases, go, distributedsystems, architecture
---

Most programs keep documents as JSON in a relational column, with the database's
JSON-path support standing in for a document API. The row has one schema, the document
another. A read is SQL wrapped around a path expression; a write replaces a column the
database cannot patch; change notification, audit and undo are each a mechanism built
*beside* the data. Every step negotiates between two models, and the negotiation is where
the incoherency lives.

`logd` makes the document the unit of storage. It is the append-only commit store under
[docd](https://signadot.github.io/tony-format/docd/), part of the
[Tony format](https://github.com/signadot/tony-format) project. Every write is a numbered
commit; state is folding patches over snapshots. This post is logd alone — docd, the
layer composing a document across many owners, is its own post.

---

## 1. The indexed event representation

Storing a document by serializing the tree works until the document is large and the
write is small. Then every read pays for the whole thing.

logd stores **event streams**:

```
BeginObject
  Key("name")  String("widget")
  Key("qty")   Int(5)
  Key("tags")  BeginArray  String("a")  String("b")  EndArray
EndObject
```

Thirteen event types, including `HeadComment` and `LineComment` — comments are events,
which is why they survive to disk. Tags ride on the values they decorate.

A snapshot is one value's events plus an index into them:

```
[ header ][ events ][ directory ][ index ]
```

The directory is a table of children per container — each child's name, kind, and where
its events are — written as each container closes, so a listing of a container reads its
table rather than its events.

The index is `(kinded path, byte offset)` entries: root at 0, then one per ~4 KiB of
events. **Size-bound** — it grows with bytes over chunk size, not node count — so a
multi-gigabyte snapshot still indexes in memory. Paths are *kinded*: `.field`, `[3]`
dense, `{3}` sparse, `(sku=A)` for a keyed element named by identity. A read seeks the
nearest entry at or before the path and decodes forward, touching a chunk.

### The log index

That answers where a path is in a file. A second answers which writes since the snapshot
can affect a subtree: a trie over the document's structure holding `(path, commit range,
log file, offset)`. A patch is indexed at every path on the way to what it writes, marked
**Spine** (passed through) or **Statement** (said something here). A read below a spine
path is unaffected by it — which is what lets a read at `verse.entities.e1` skip every
write to `e2`. A read at a path never written opens no file: the spine proves absence.

Millions of commits do not fit in memory, so the durable regions file is the truth and
the resident trie an LRU cache of it, under one invariant:

> EVICTION CHANGES A COST, NEVER AN ANSWER.

Not tidiness: one commit was measured at 5.8 seconds in its index phase because
compaction walked the trie holding the root's read lock while writes landed.

---

## 2. Streaming diff application

A read is three steps, each bounded:

```
seek     the nearest snapshot at or below the commit, opened AT the path
project  the writes since it that can reach the path — and no other — one record
         at a time, each cut down to what it says about the path
fold     one streaming pass, base events with those writes applied over them
```

Resident: the projected writes, and one record. Neither counts how much has happened
under the path.

A collector watches the stream's position; entering a path with a patch pending, it
materializes *that subtree only*, patches it, emits it. Everything else passes through
event by event. A one-field change to a 100 MB document materializes the field's parent.

The same fold serves a read, the next snapshot's construction, and the watch consumer
applying deltas. Three appliers that disagree is a class of bug.

### What the log may keep

A log replays, so **a delta the store cannot apply is not a failed write — it is a
permanent one.** Every later read meets it, including reads of documents it never
touched, since all replay through the same log; no later patch repairs it, because the
read dies on the way past. So every patch is applied before it is stored:

```tony
v: !arraydiff {0: 99}                 # on [1, 2] — applies, and always will
v: !arraydiff {5: 99}                 # on [1, 2] — refused, invalid_diff
```

And what is *kept* is narrower than what may be written: only operations stating what the
value **is**. One whose meaning depends on what it lands on is **lowered** — applied, its
result stored in its place.

```tony
# baseline holds {s: bob}
s: !replace {from: bob, to: rob}    # stored as  s: !insert rob
```

Nearly free — the read lowering needs was taken anyway, to refuse patches that do not
apply — and it buys total replay: a stored delta re-applied to a moved base gives what it
gave at the write, forever.

---

## 3. The A/B log files

Two files. One **active**, taking appends; one **inactive**, quiet enough to rewrite.
Both durable — not scratch-and-rotate. They alternate, and each round the quiet one gets
a snapshot and a pruning:

```
a commit lands
  → thresholds checked (maxCommits 1000, maxBytes 4 MiB of delta)
    → active log switched, so writes keep landing in the new one
      → baseline snapshot written into the log that just went inactive
        → that log compacted
```

The snapshot is the part-2 fold: previous snapshot's events, project the writes since,
fold, write into the log through a builder that indexes as bytes go by. The document is
never materialized — not to read, not to snapshot. Snapshots live inside the log files as
blobs behind a magic no record length can take.

Compaction rewrites a log in place while readers hold offsets into it. Three pieces make
that safe: a **generation** per file, bumped on compaction and recorded on every index
segment; a **reader refcount**; and the replaced file kept **open and unlinked**, serving
old-generation reads until that file's next compaction, which waits for readers to drain
or for the grace period (5s). A read crossing a swap neither reads stale bytes nor fails.

### Snapshots the reads ask for

The root snapshot's cadence is the whole document's, which is wrong for a hot subtree. So
**per-path** snapshots too, scheduled by reads: a read folding more records at a path
than the policy prices its subtree at schedules one there, and the next read folds from
it. The default is a *rate* — 64 records per 1 MiB of subtree — not a ceiling. Reads
measure the tail for free, and a path nobody reads costs nothing.

---

## 4. Compaction

> Compaction removes the account of **how** the state got there. Never the state.

The switch writes a full snapshot at the switch commit immediately before compacting.
Moments old, always inside the window, always survives; everything the dropped records
contributed is in it.

The schedule is logarithmic. Within a **cutoff** (default 1h) every delta is kept, so
reads and replays are exact to the commit. Past it, history degrades to snapshot
granularity in geometrically widening tiers — by default 8 snapshots in the first hour
past cutoff, 8 in the next two, 8 in the next four, newest first. A `horizon` bounds the
whole thing.

The cost:

- **current** state is exact, always, at any setting;
- a **historical** commit below the cutoff is approximate — it lands on the nearest
  surviving snapshot;
- a watch resuming below retained history is refused `replay_compacted`, not served a
  gap.

The replay floor is raised *before* anything is dropped, so a crash between the two
over-reports the loss. A spurious refusal beats silent event loss.

Two sharp edges. Compaction is **off** unless configured, and runs **only** as a
snapshot's last step — no timer, no manual trigger, so a store that stops taking writes
never compacts. And its work list is the log's own records, not the index: walking the
index for every segment means holding the whole store — the 5.8-second stall above.

---

## 5. Retention

Compaction does not help with log-like data — jobs, runs, events. A record nobody deletes
is *state*.

The decision I like best here: **retention is a request, and the request is a write.**
logd holds no policy and no clock. The caller carries both.

```tony
{retain: {
  now: "2026-09-12T08:00:00Z"        # optional; the server's clock when absent
  what:
  - path: jobs.*
    match: {status: !or [done, canceled]}
    age: .updatedAt
    after: 1d
}}
```

logd reads the container, finds items older than the rule allows, and commits an
**ordinary delete**. Everything true of a client's delete holds: watchers see deltas with
an author, the schema can refuse one, a read at an older commit still shows the record,
each batch carries a precondition on what the pass read. The commit *is* the record that
it ran, and a pass given an explicit `now` is reproducible from the log.

Age comes from the record, not the store, because once compaction drops a delta past the
cutoff the index no longer knows when it was written — and with a one-hour cutoff and a
one-year limit those are exactly the records a rule is for.

A rule's last segment is `.*`, `{*}` or `(*)`. Dense `[*]` is refused: an index names a
position, and a concurrent insert lands the expiry on a neighbour.

Retention does not free disk alone — older snapshots still carry the record. Pair it with
`compaction.horizon`.

---

## 6. Copy-on-write scopes

A **scope** is an isolated layer over the shared baseline. A session picks one at
handshake; reads then see baseline with the scope's writes last, and writes land in the
scope, invisible to baseline and to every other scope.

What matters is what a scope is *not*. Not a branch, not a snapshot. Baseline keeps
moving underneath, and the scope keeps seeing it — except where it has written:

```tony
# baseline holds {a: {x: 1, y: 2}}
# the scope writes  {a: {x: 5}}
# baseline then writes {a: {x: 99, y: 7}}

baseline reads  {a: {x: 99, y: 7}}
the scope reads {a: {x: 5,  y: 7}}
```

The scope keeps its `x` and picks up baseline's new `y`. A scope's write shadows *later*
baseline writes at that path, and only there — which is what makes it useful for trying a
change against a store other clients are still writing to: the shape of a development
sandbox over a shared environment.

Hence a scope stores something different. A baseline delta replays against a base that
never moves, so the **difference** is sound to store. A scope's base moves, so a scope
stores the **claim**: what it holds at that path, whatever baseline does next.

|  | replay | what is stored |
|---|---|---|
| baseline | deterministic | the difference the write made |
| scope | base moves | the claim the write made |

Load-bearing. A scope deleting a field baseline has not created yet makes no *difference*
to state — but it makes a claim, and without storing it the scope stops shadowing that
path the moment baseline creates the field.

A scoped read folds the scope's patches over baseline, bounded by the **footprint**: a
stored scope write is absolute, so a later statement dominates what it covers, and the
index keeps per scope the statements no later one dominates. So a scoped read costs what
the scope *holds* at a path, not its history.

The cost: compaction never drops scope records, because for a scope the records *are* the
state. A long-lived scope grows without bound. Open issue.

---

## How this compares

etcd and Postgres are complete systems; logd is a storage engine with a session protocol
on it — closer to putting MyISAM, without mysqld, next to Postgres. The layer that routes
and composes across logd is **docd**: a separate post.

So claims about *representation* — diffs, paths, scopes, what a watch carries, what a
precondition costs — are engine-level. Absences belonging a layer up — a planner,
composition across owners, cluster membership — are scope, not verdict.

### vs. etcd

Flat keyspace, opaque values, MVCC by revision, raft.

**The value is not opaque.** Changing one field of a nested document in etcd is
read-modify-write of the whole thing. The workaround is exploding the document into a key
prefix — trading atomicity, ordering and schema for granularity, then rebuilding the
document in every client.

**Watches deliver diffs.** An etcd `PUT` carries the new value in full: flip one field of
a 100 KB document and every watcher gets 100 KB, then diffs it itself. A logd watch
delivers one commit's delta, rooted at the watched path, ordered and gapless.

**Preconditions cost what the pattern names** — shape, or named fields. Only a pattern
over the whole value reads it, so writing one entity into a parent holding thousands is
not charged for the thousands. etcd compares a revision or a value, and comparing ships
it.

**Scopes have no equivalent.** Copy a keyspace to another prefix and the copy is frozen;
baseline moves on and you maintain a merge.

**Secondary indexes: hand-built and atomic in both.** Couple the index write to the
entity write as two participants of one transaction — same commit, every precondition
checked together, all or nothing; etcd's multi-key txn gets the same. Neither has a
planner that uses the index unasked, which is the real gap against a relational database.

**Leases are etcd's, for now.** Native expiry via keepalives is how ephemeral
registration and leader election get built; logd lacks it (`git issue show qqq1jejg`).
Not the same as retention, though: a TTL answers less than a rule expiring items that
match a *pattern*, with age read from the record and policy and clock owned by the
caller. What retention cannot express is *liveness*. Two capabilities, not one gap.

**HA.** etcd is raft: linearizable across nodes, correct under partition. logd is one
process, and the direction is not raft — which floors failover at the election timeout
and adds a configuration surface that is itself a common cause of outages. The plan is
active/passive over shared storage, failover bounded by an `flock` (`git issue show
nkn7ptxc`). What you give up is a real cluster.

**Where etcd is further along.** A decade behind Kubernetes, documented limits, backup
and restore, clients everywhere. logd's own docs are blunt: young, not yet the store to
put under a system that cannot afford one.

### vs. JSON in a relational column

**One model instead of two** — a write is a patch, a read is a match, the schema is a
document in the format of the data it governs.

**Partial writes are partial.** `jsonb_set` is server-side, but MVCC still writes a new
tuple version, and a large TOASTed value is rewritten for a one-field change.

**Change notification is the data's own history.** `LISTEN`/`NOTIFY` caps payloads at
8000 bytes and is not durable: a disconnected listener missed everything, with no way to
ask what. Triggers into an audit table, or logical decoding, are a second system to keep
coherent with the first. logd's watch resumes `fromCommit` off the log itself, and time
travel is a parameter rather than an audit table you design, migrate alongside your
document schema, and query differently.

**Fidelity.** `jsonb` normalizes: key order lost, duplicate keys dropped, no comments to
begin with. For config documents humans also edit, that is losing the document.

**Scopes have no clean analogue.** A long-lived transaction holds locks, bloats, is
invisible to everyone else, and cannot last days. A storage-level branch forks and
*diverges*. A scope follows baseline everywhere it has not written.

**Where the database wins.** Predicate queries: a GIN index answers "every document
containing this" across the table, planner-chosen, nothing to maintain. logd's hand-built
index is atomic but nothing chooses it, and nothing keeps it honest if you forget a write
path. So too joins, aggregates, constraints, PITR, replicas — none of it in logd, because
none of it is what logd is for. And unless your whole domain is documents, logd is a
*second* store.

### Where the line falls

logd fits when the thing you store is a **document**, **watched** and **patched by
several actors**, when history and undo should be properties rather than features built
beside the data, and when someone needs to **try a change against it while it is still
moving**. A read costs what the path and the delta cost, not what the store holds. That
is most of what a control plane does — the part Kubernetes-shaped systems solve today by
flattening documents into etcd keys, or burying them in a `jsonb` column and rebuilding
the document API on top.

Not for: analytics, large fact tables, predicate queries over collections, and — until
the holdover lands — anything that cannot tolerate a single writer.

---


## The shape of the argument

**Store the event stream, index the offsets.** A size-bound path index over flat events
gives random access into arbitrarily large documents without materializing them.

**Lower relative operations at write time.** If your log replays, store what the
operation produced, not the operation. Costs a read you were taking anyway; makes replay
total.

**Validate the delta against the state before storing it.** In an append-only log an
unapplicable record is not a failed write but a permanent one, and it poisons reads of
documents it never touched.

**A layer that tracks beats a layer that forks.** Copy-on-write over a moving base is
harder than a branch, and it is what matches how people work against shared state.

Together: history, change notification, undo, isolation and point-in-time reads are not
features built beside the data. They are what the data is.

---

*logd and the Tony format are open source at
[github.com/signadot/tony-format](https://github.com/signadot/tony-format); storage docs
at [signadot.github.io/tony-format/logd](https://signadot.github.io/tony-format/logd/).
Issues live in the repository — `git issue list`.*
