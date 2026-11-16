---
title: "A document store built out of a log: events, diffs, and two files"
published: false
status: draft
description: "logd stores documents as indexed event streams, applies writes as streaming diffs, and backs snapshots with two alternating log files. How that works, and how it compares to etcd and JSON-in-a-column."
tags: databases, go, distributedsystems, architecture
---

Most programs that keep documents keep them as JSON in a relational column, with the
database's JSON-path support standing in for a document API. That is a second model
between the program and its data. The row has one schema and the document another. A
read is SQL around a path expression, and a write replaces a column the database cannot
patch. Change notification, audit and undo are each a mechanism built beside the data
rather than a property of it. Every step negotiates between the two models, and the
negotiation is where the incoherency lives.

`logd` is a store whose unit is the document. It is the append-only commit store beneath
[docd](https://signadot.github.io/tony-format/docd/), and part of the
[Tony format](https://github.com/signadot/tony-format) project. Every write is a
numbered commit, and state is reconstructed by applying patches over snapshots.

This article describes logd alone: how it represents a document, how it applies a write,
how it keeps its log bounded, and how it compares to etcd and to JSON in a relational
column. docd, the layer which composes one document across many owners, is the subject
of a separate article.

## The indexed event representation

A store can keep a document by serializing its tree. That works until the document is
large and the write is small, at which point every read pays for the whole document.

logd instead stores a document as a stream of events:

```
BeginObject
  Key("name")  String("widget")
  Key("qty")   Int(5)
  Key("tags")  BeginArray  String("a")  String("b")  EndArray
EndObject
```

There are thirteen event types. Two of them, `HeadComment` and `LineComment`, are
comments, which is why a comment in a document survives being stored. Tags are carried
on the values they decorate.

### Snapshots

A snapshot is the events of one value, together with what is needed to read part of it:

```
[ header ][ events ][ directory ][ index ]
```

The directory is a table of children for each container. It gives each child's name, its
kind, and where its events are. A container's table is written as the container closes,
so listing a container reads its table rather than its events.

The index is a list of `(kinded path, byte offset)` entries: the root at offset 0, and
then one entry for about every 4 KiB of events. Its size therefore depends on the number
of bytes divided by the chunk size, and not on the number of nodes, so the index of a
multi-gigabyte snapshot still fits in memory. A read seeks to the nearest entry at or
before its path and decodes forward from there, which touches one chunk.

Paths are *kinded*, meaning that each segment says what kind of container it steps
into:

```
.field      a field of an object
[3]         an element of a dense array, by position
{3}         an entry of a sparse array
(sku=A)     an element of a keyed array, named by identity
```

### The log index

The snapshot index answers where a path is within one file. A second index answers a
different question: which writes since the snapshot can affect a given subtree.

This index is a trie over the document's structure. Each node holds entries of the form
`(path, commit range, log file, offset)`. A patch is indexed at every path on the way to
what it writes, and each entry is marked as one of two things:

- **Spine**: the patch passed through this path on its way to something deeper.
- **Statement**: the patch said something at this path.

A read below a spine path is unaffected by that entry. This is what allows a read at
`verse.entities.e1` to skip every write to `verse.entities.e2`. A read at a path that
was never written opens no file at all, because the spine is enough to show that nothing
is there.

Millions of commits do not fit in memory. The durable regions file is therefore the
truth, and the trie in memory is an LRU cache of it. The cache operates under one
invariant:

> EVICTION CHANGES A COST, NEVER AN ANSWER.

Writers share the trie with readers, so how the trie is walked matters as much as what
it holds. In one measurement a single commit spent 5.8 seconds in its index phase,
because compaction was walking the whole trie while holding the root's read lock, and
writes were landing at the same time. A walk now locks one node at a time, and
compaction no longer walks the index at all (see [Compaction](#compaction)).

## Streaming diff application

A read has three steps, and each is bounded:

```
seek     the nearest snapshot at or below the commit, opened AT the path
         (beyond compaction's cutoff, the first at or after it)
project  the writes since it that can reach the path, and no others, one record
         at a time, each cut down to what it says about the path
fold     one streaming pass: the base events, with those writes applied over them
```

What a read holds in memory is the projected writes and one record. Neither grows with
the history of the path: snapshots bound the first, and the second is a single record.

The fold works as follows. A collector watches the position of the stream. When the
stream enters a path which has a patch pending, the collector materializes that subtree
only, patches it, and emits the result. Everything else passes through one event at a
time. For example, a change to one field of a 100 MB document materializes the parent of
that field and nothing more.

The same fold serves three purposes: a read, the construction of the next snapshot, and
a watch consumer applying deltas. This is deliberate. Three appliers which disagree with
one another are a class of bug, and one fold cannot disagree with itself.

### What the log may keep

logd is a log, and a log is replayed. For this reason a delta which the store cannot
apply is not a failed write. It is a permanent one. Every later read meets it again,
including reads of documents the write never touched, since all reads replay through
the same log. No later patch repairs it either, because the read fails on its way past
the bad record.

So every patch is applied before it is stored, to the state it would be stored against:

```tony
v: !arraydiff {0: 99}                 # on [1, 2]: applies, and always will
v: !arraydiff {5: 99}                 # on [1, 2]: refused, invalid_diff
```

What the log *keeps* is also narrower than what a client may *write*. The log keeps only
operations which state what the value **is**. An operation whose meaning depends on what
it lands on is **lowered**: it is applied, and its result is stored in its place.

```tony
# baseline holds {s: bob}
s: !replace {from: bob, to: rob}    # stored as  s: !insert rob
```

Lowering costs very little. It needs a read of the state, and that read was already
being taken in order to refuse patches which do not apply. In return, replay is total: a
stored delta re-applied to a base that has moved gives what it gave at the write.

## The A/B log files

logd keeps two log files. At any time one is **active** and takes appends, and the other
is **inactive**, which makes it quiet enough to rewrite. Both are durable; neither is
scratch space. The two alternate, and on each round the quiet one receives a snapshot
and is then compacted:

```
a commit lands
  → thresholds checked (maxCommits 1000, maxBytes 4 MiB of delta)
    → active log switched, so writes keep landing in the new one
      → baseline snapshot written into the log that just went inactive
        → that log compacted
```

The snapshot is built by the fold described above. logd takes the events of the previous
snapshot, projects the writes since, folds them, and writes the result into the log
through a builder which indexes the bytes as they go by. The document is never
materialized, either to read it or to snapshot it. Snapshots live inside the log files
as blobs, behind a magic value which no record length can take.

### Rewriting a file that is being read

Compaction rewrites a log file in place, while readers hold offsets into it. Three
pieces make this safe:

- a **generation** for each file, which is incremented by a compaction and recorded on
  every index segment;
- a **reader refcount** for each file;
- the replaced file itself, which is kept **open and unlinked** and serves reads at the
  old generation.

The replaced file is closed by the next compaction of the same file, which first waits
for its readers to finish or for a grace period (5s by default) to pass.

A read which crosses a swap is therefore served the bytes it was indexed against. A read
which outlives the *next* swap of that file, and its grace period, is refused with an
error. In no case is a read served stale bytes.

### Snapshots that reads ask for

The root snapshot is taken at a cadence which belongs to the whole document, and that
cadence is wrong for a subtree which is written often. logd therefore also takes
snapshots of individual paths, and these are scheduled by reads.

A read schedules a snapshot at its path when it folded more records there than the
policy prices a subtree of that size at. The next read at the path folds from the new
snapshot. The default is a *rate* and not a ceiling: 64 records for each 1 MiB of
subtree. A read measures the length of what it folded at no extra cost, and a path that
nobody reads costs nothing.

## Compaction

An append-only log does not have to be unbounded. Compaction is what bounds it, and it
operates under one rule:

> Compaction removes the account of **how** the state got there. Never the state.

Immediately before a log is compacted, the switch writes a full snapshot of the state at
the switch commit. That snapshot is moments old, so it always falls inside the retention
window and always survives. Everything that the removed records contributed is in it.

### The schedule

The schedule is logarithmic. Within a **cutoff** (1h by default), every delta is kept,
so reads and replays in that window are exact to the commit. Beyond the cutoff, history
degrades to the granularity of snapshots, which are kept in tiers that widen
geometrically. With the defaults, 8 snapshots are kept from the first hour beyond the
cutoff, 8 from the next two hours, and 8 from the next four. Each tier keeps its most
recent snapshots. An optional `horizon` sets how far back any history survives at all.

### What it costs

- Reading the **current** state is exact, always, at any setting.
- Reading a **historical** commit beyond the cutoff is approximate. The read is answered
  at the first surviving snapshot at or after that commit.
- A watch which resumes from below the retained history is refused with
  `replay_compacted`, rather than being served a gap.

The replay floor is raised *before* any record is dropped. A crash between the two steps
therefore over-reports what was lost. This ordering is chosen because a spurious refusal
is preferable to the silent loss of events.

### Two things to know before relying on it

First, compaction is **off** unless the configuration asks for it. It also runs only as
the last step of taking a snapshot. There is no timer and no way to run it by hand, so a
store which stops taking writes never compacts.

Second, the work list of a compaction is the log's own records, and not the index.
Asking the index for every segment it holds pages the whole store into memory, and that
walk is the one behind the 5.8 second stall described under
[The log index](#the-log-index).

## Retention

Compaction does not help with data that is log-like, such as jobs, runs and events. A
record which nobody deletes is *state*, and compaction never removes state.

In logd, retention is a request, and the request is a write. logd holds no retention
policy and no clock for one. The caller carries both:

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

logd reads the container, finds the items which are older than the rule allows, and
commits an **ordinary delete** for them. Everything that is true of a client's delete is
true of these:

- watchers see them as deltas, carrying an author;
- the schema still applies, and can refuse one;
- a read at an older commit still shows the records;
- each batch carries a precondition on what the pass read.

The commit is itself the record that the pass ran, and a pass which was given an
explicit `now` is reproducible from the log.

### Why age comes from the record

A rule reads the age of an item from a timestamp in the item, rather than asking the
store how old the item is. The store cannot answer that question. Once compaction drops
a record's delta beyond the cutoff, the index no longer knows when the record was
written. With a cutoff of one hour and a limit of one year, those are exactly the
records a rule exists for.

### What a rule may name

The last segment of a rule's path is `.*`, `{*}` or `(*)`. A dense array, `[*]`, is
refused. An index names a position and not an element, so a concurrent insert before
that position would land the expiry on a neighbour.

### Disk

Retention does not free disk on its own, because older snapshots still carry the deleted
record. To have the record leave the disk as well, pair retention with
`compaction.horizon`.

## Copy-on-write scopes

A **scope** is an isolated layer over the shared baseline. A session selects one in its
handshake. From then on its reads see baseline with the scope's own writes applied last,
and its writes land in the scope, where they are invisible to baseline and to every
other scope.

A scope is not a branch, and it is not a snapshot. Baseline keeps moving underneath it,
and the scope keeps seeing those changes, except where the scope has written something
itself:

```tony
# baseline holds {a: {x: 1, y: 2}}
# the scope writes  {a: {x: 5}}
# baseline then writes {a: {x: 99, y: 7}}

baseline reads  {a: {x: 99, y: 7}}
the scope reads {a: {x: 5,  y: 7}}
```

The scope keeps its own `x` and picks up baseline's new `y`. A scope's write shadows
*later* baseline writes at that path, and only at that path. This is what makes a scope
useful for trying a change against a store that other clients are still writing to. It
has the shape of a development sandbox over a shared environment.

### What a scope stores

Because of this, a scope stores something different from what baseline stores. A
baseline delta is replayed against a base which never moves, so it is sound to store the
**difference** the write made. A scope's base does move, so a scope stores the
**claim**: what the scope holds at that path, whatever baseline does next.

|  | replay | what is stored |
|---|---|---|
| baseline | deterministic | the difference the write made |
| scope | base moves | the claim the write made |

The distinction is load-bearing. Consider a scope which deletes a field that baseline
has not created yet. The delete makes no *difference* to the state, but it does make a
claim. If the claim were not stored, the scope would stop shadowing that path at the
moment baseline created the field.

### What a scoped read costs

A scoped read folds the scope's patches over baseline. The fold is bounded by the
scope's **footprint**, which is the set of statements in the scope that no later
statement dominates. A stored scope write is absolute, so a later statement dominates
whatever it covers, and the index keeps the footprint for each scope as writes arrive.
A scoped read therefore costs what the scope *holds* at a path, and not the scope's
history there.

### Scopes and compaction

Compaction treats a scope's records differently from baseline's, because for a scope the
records *are* the state. No snapshot stands in for them. A statement which is still live
in the footprint is kept until the scope is deleted. Beyond the cutoff, an entry that
later entries dominate is dropped. The log of a long-lived scope is therefore bounded by
what the scope holds, and not by how often the scope rewrote it.

## Comparison to etcd and Postgres

etcd and Postgres are complete systems, and a managed database such as RDS is more
complete still. logd is lower level than any of them. It is a backend: a storage engine
with a session protocol on it, which expects a more frontend layer above it. **docd**,
which routes and composes across logd, is one such layer. A document-oriented
application's own frontend may fill that gap instead, and may do more than docd does.

The comparisons below should be read with that in mind. Claims about *representation*
are claims about the engine: diffs, paths, scopes, what a watch carries, and what a
precondition costs. Things which belong a layer up, such as a query planner, composition
across owners, or cluster membership, are outside logd's scope. Their absence is not a
judgement of them.

### Comparison to etcd

etcd provides a flat keyspace of opaque values, with MVCC by revision, replicated by
raft.

#### Values

In etcd a value is opaque. Changing one field of a nested document is a read, a
modification, and a write of the whole value. The usual workaround is to explode the
document into keys under a prefix. That trades atomicity, ordering and schema for
granularity, and every client must then rebuild the document. In logd the value is a
document, and a write is a patch to it.

#### Watches

An etcd `PUT` event carries the new value in full. If one field of a 100 KB document
changes, every watcher receives 100 KB and computes the difference itself. A logd watch
delivers the delta of one commit, rooted at the watched path, in order and without gaps.

#### Preconditions

A logd precondition costs what its pattern names: the shape of the value, or the fields
it names. Only a pattern over the whole value reads the whole value, so writing one
entity into a parent which holds thousands is not charged for the thousands.

An etcd revision compare is cheap, but it guards the whole key. It cannot say "this
field is unchanged" while a sibling field changes. An etcd value compare can say more,
and ships the value in order to say it.

#### Scopes

etcd has no equivalent of a scope. A keyspace can be copied to another prefix, but the
copy is frozen. Baseline moves on, and the client maintains a merge.

#### Secondary indexes

Like etcd, logd leaves secondary indexes to the client, and like etcd it can keep one
atomically. In logd the index write and the entity write are two participants of one
transaction: they share a commit, every precondition is checked together, and either all
of it is written or none. etcd's multi-key transaction provides the same. Neither has a
planner which uses the index unasked, and that is the real gap between both of them and
a relational database.

#### Leases

etcd has leases and logd does not yet (`git issue show qqq1jejg`). Native expiry driven
by keepalives is how ephemeral registration and leader election are built.

A lease is not the same thing as retention. A TTL says less than a retention rule, which
expires the items matching a *pattern*, reads age from the record, and leaves policy and
clock with the caller. What retention cannot express is *liveness*. These are two
capabilities, and logd has one of them.

#### High availability

etcd is replicated by raft. It is linearizable across nodes and correct under partition.

logd is one process, and the direction for it is not raft. Raft puts a floor under
failover at the election timeout, and it adds a configuration surface which is itself a
common cause of outages. The plan is instead active/passive over shared storage, with
failover bounded by an `flock` (`git issue show nkn7ptxc`). What this gives up is a real
cluster.

#### Maturity

etcd has spent a decade under Kubernetes. It has documented limits, backup and restore,
and clients everywhere. logd is young. As its own documentation says, it is not yet the
store to put under a system that cannot afford one.

### Comparison to JSON in a relational column

#### One model

In logd a write is a patch, a read is a match, and the schema is a document in the
format of the data it governs. There is one model, where a JSON column has two.

#### Partial writes

`jsonb_set` runs on the server, but MVCC still writes a new version of the tuple, and a
large TOASTed value is rewritten for a change to one field. In logd a partial write is
stored as a partial write.

#### Change notification

`LISTEN`/`NOTIFY` limits a payload to 8000 bytes and is not durable. A listener which
was disconnected has missed everything sent in the meantime, and has no means of asking
what that was. Triggers which write to an audit table, or logical decoding, are a second
system to keep coherent with the first.

A logd watch resumes from `fromCommit` using the log itself. Reading the past is a
parameter of a read, rather than an audit table which must be designed, migrated
alongside the document schema, and queried differently.

#### Fidelity

`jsonb` normalizes what it stores. Key order is lost, duplicate keys are dropped, and
there are no comments to begin with. For configuration documents which humans also edit,
that amounts to losing the document.

#### Scopes

A relational database has no clean analogue of a scope. A long-lived transaction holds
locks, causes bloat, is invisible to everyone else, and cannot last for days. A branch
at the storage level forks and then *diverges*. A scope follows baseline everywhere that
it has not written.

#### Where the database is better

Predicate queries are the clearest case. A GIN index answers "every document containing
this" across a table. The planner chooses it, and nothing has to maintain it. An index
built by hand in logd is atomic, but nothing chooses it, and nothing keeps it correct if
one write path forgets it.

The same holds for joins, aggregates, constraints, point-in-time recovery and replicas.
logd has none of these, because none of them is what logd is for. Also, unless the whole
of a domain is documents, logd is a *second* store beside the database.

### Where logd fits

logd fits when what is stored is a **document** which is **watched** and **patched by
several actors**; when history and undo should be properties of the data rather than
features built beside it; and when someone needs to **try a change against the store
while it is still moving**. A read costs what its path and the delta cost, and not what
the store holds.

That describes most of what a control plane does. Today, systems shaped like Kubernetes
solve it by flattening documents into etcd keys, or by putting them in a `jsonb` column
and rebuilding a document API on top.

logd is not for analytics, large fact tables, or predicate queries over collections.
Until active/passive failover lands, it is also not for anything which cannot tolerate a
single process.

## Summary

Four ideas carry most of the design.

- **Store the event stream and index the offsets.** A path index over flat events, whose
  size depends on bytes and not on nodes, gives random access into arbitrarily large
  documents without materializing them.
- **Lower relative operations when they are written.** In a log that is replayed, store
  what an operation produced rather than the operation. This costs a read which was
  being taken anyway, and it makes replay total.
- **Validate a delta against the state before storing it.** In an append-only log, a
  record which does not apply is not a failed write but a permanent one, and it breaks
  reads of documents it never touched.
- **Prefer a layer that tracks to a layer that forks.** Copy-on-write over a moving base
  is harder to build than a branch, and it matches how people work against shared state.

Taken together, these make history, change notification, undo, isolation and
point-in-time reads properties of the data, rather than features built beside it.

---

*logd and the Tony format are open source at
[github.com/signadot/tony-format](https://github.com/signadot/tony-format). The storage
documentation is at
[signadot.github.io/tony-format/logd](https://signadot.github.io/tony-format/logd/).
Issues are kept in the repository itself and are listed with `git issue list`.*
