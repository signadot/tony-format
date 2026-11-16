---
title: "Applying structured diffs to an event stream without building the document"
published: false
status: draft
description: "How logd folds stored patches onto a snapshot's events: where a patch states something, which subtrees are materialized, what happens to a key whose value is deleted, and how a write to a path the base never reaches is placed."
tags: databases, go, algorithms, architecture
---

`logd` is the append-only commit store beneath
[docd](https://signadot.github.io/tony-format/docd/), part of the
[Tony format](https://github.com/signadot/tony-format) project. It keeps a document as a
snapshot plus the patches written since. A companion article describes the store as a
whole. This one describes a single algorithm in it: how the patches are applied to the
snapshot.

The algorithm is called the **fold**. It reads the snapshot as a stream of events, and
writes a stream of events, and it never builds the document in between. This article
covers applying a patch only. Producing one, by diffing two documents, is a different
algorithm and is not described here.

## The problem and the bound

A read in logd is at a path and at a commit. To answer it, the store needs the state of
the subtree at that path: the nearest snapshot, with every later write that affects the
path applied to it.

The simple way to do this is to decode the snapshot into a tree, apply each patch to the
tree, and return the tree. The cost of that is the size of the document, for every read,
however small the path or the change.

The fold is held to a different bound. What a read holds in memory is

- the writes since the snapshot that can reach the path, each cut down to what it says
  about the path, and
- one log record at a time while those are being gathered.

Neither term is a count of how much the document holds, or of how much has happened
elsewhere in it. For this reason a read does not return a document. It returns a
**cursor** over the events of the subtree, because a function that returns a document
cannot keep a bound of this kind.

## The three inputs

### The event stream

A snapshot stores a value as a flat sequence of events:

```
BeginObject
  Key("a")  BeginObject
              Key("x")  Int(1)
              Key("y")  Int(2)
            EndObject
  Key("b")  Int(3)
EndObject
```

There are thirteen event types: the begin and end of an object and of an array, a key,
an integer key (for a sparse array), five scalars, and two kinds of comment.

A small state machine follows the stream and knows the **kinded path** of the current
position. A kinded path is a path in which each segment says what kind of container it
steps into: `.field`, `[3]` for a position in an array, `{3}` for an entry of a sparse
array, and `(sku=A)` for an element of a keyed array.

The timing of the path matters for what follows. A `Key("x")` event sets the current
path to `a.x` at once. The value event after it leaves the path unchanged. So the path
`a.x` is first seen on the key, and not on the value.

### A stored patch

A stored patch is a tree in the same data model as the document. It is rooted at the
document, and its shape says where it applies:

```tony
{a: {x: 5}}
```

Nothing in the patch marks where the change is. The next section gives the rule by which
that is read from its shape.

### The sink

The output is an event writer. For a read it is a cursor, and for a snapshot it is a
builder which indexes the bytes as they are written.

## Where a patch states something

A patch may state something at several places. Each such place is called a **root** of
the patch. The rule for finding them is a walk from the top of the patch:

| the node is | what it means |
|---|---|
| an operation, such as `!insert` or `!delete` | a statement at this path; its operand is not descended into |
| a scalar | a write at this path |
| an array | a value, and a write at this path |
| an empty object | a statement of emptiness at this path |
| a node with a comment | one statement at this path, comment and value together |
| an object with fields | not a root; the walk continues into each field |

An object with fields says only that something beneath it differs. Merging it at its
path is the same as merging its parts at theirs.

For example:

```tony
{a: {x: 5}}             # one root, at a.x
{a: !insert {z: 1}}     # one root, at a
{a: {x: 5}, b: [1, 2]}  # two roots, at a.x and at b
```

Because the shape is the statement, a stored patch carries no annotation which would
have to be removed before it is applied or delivered. The bytes that are stored are the
bytes a watch delivers.

## Projection

A read at a path does not need every patch in the log, and it does not need all of the
patches it does need. Gathering what it needs is called **projection**, and it has two
parts.

First, the log index answers which records can reach the path. The index is described in
the companion article. Its result is a sequence of log positions, in commit order, for
exactly the writes at the path, above it, or beneath it.

Second, each of those records is read from the log, one at a time, and cut down to what
it says at or below the path. The result is re-rooted at the path. For a read at `a`:

```tony
{a: {x: 5}, b: 1}       # projects to {x: 5}
{b: 2}                  # says nothing about a, and is skipped
{a: !insert {z: 1}}     # projects to !insert {z: 1}
```

The record itself is then released. What remains in memory is the list of projected
patches, whose size is what changed under the path since the snapshot.

The same rule is used wherever a delta is re-rooted. A watch projects a stored entry
onto the path it watches by this rule, and docd projects a composed delta onto a
client's path by it. A delta that a client receives for a path is therefore a delta at
that path, whatever produced it.

### When a write above the path blocks projection

There is one case in which a subtree cannot answer for itself. Suppose a read is at
`a.x`, and a write has an operation at `a`:

```tony
{a: !insert {z: 1}}
```

The operation states the whole value at `a`. Its operand is not a patch to descend into,
so the write cannot be cut down to what it says about `a.x`. Projection reports that it
is blocked, and at which ancestor.

Such a read is taken at the ancestor instead. The value at `a` is read in full, and the
read then steps down from it to `a.x`. The ancestor's value is held in memory while this
is done. It is the value which that write installed, and it is the one large term which
the bound admits.

## Dominance

The fold receives the projected patches in commit order. It first finds the roots of
each, and so obtains a set of paths at which something is stated.

These paths may be nested. In the following three writes, the second has a root at `a`,
and the other two have roots beneath it:

```tony
{a: {x: 5}}
{a: !insert {z: 1}}
{a: {y: 7}}
```

A root which has another root as a strict ancestor is said to be **dominated**. The
subtree at the dominating path is going to be collected and patched as a unit, so the
dominated roots cannot be applied separately at their own paths.

They cannot be dropped either. An earlier version of the fold dropped them, and the
effect was that a write to an ancestor erased every write to a descendant made since the
snapshot.

Instead, the fold keeps only the **maximal** paths, those with no ancestor in the set.
For each maximal path, every patch is navigated to that path and contributes what it
holds there, in commit order. In the example the one maximal path is `a`, and the three
patches contribute `{x: 5}`, then `!insert {z: 1}`, then `{y: 7}`. Applied in that order
to a base of `{a: {x: 1, y: 2}}`, they give

```tony
{a: {y: 7, z: 1}}
```

which is what applying the three writes to the document one after another gives.

### Finding the maximal paths

A path is dominated by at most one maximal path. The ancestors of a path form a chain,
so if two maximal paths dominated it, one would be an ancestor of the other, and the
deeper one would not be maximal.

This allows the maximal paths to be found in one pass. The paths are sorted segment by
segment. In that order a path comes immediately before its descendants, and anything
which sorts between a path and one of its descendants is also a descendant. The pass
therefore remembers only the last maximal path it saw, and tests each path against that
one.

Two details of this are worth recording, because each was first done the simpler way and
measured.

- The paths must be compared as parsed segments and not as strings. In string order `-`
  sorts below `.`, so `a-b` falls between `a` and `a.b` without being beneath `a`.
- Comparing every pair of paths is quadratic in the number of distinct paths the log
  range touches. Re-parsing a path at each comparison is worse: on a server replaying a
  long log, path parsing was half of the CPU. Each path is now parsed once.

## The fold

With the maximal paths and their ordered patches in hand, the fold is one pass over the
base events. Each event is treated in one of four ways: it is passed through, it is
held, it is collected, or it is dropped because a patched subtree was emitted in its
place.

```
for each event of the base:
    advance the path state
    if a subtree is being collected:
        add the event to it
        if the subtree is complete:
            build it as a tree, apply its patches in order
            if the result is absent:  forget the held key
            else:                     write the held key, then the result as events
        continue
    if the event is a key whose path has patches:
        hold the key; the next value starts a collection
        continue
    if the event starts a value whose path has patches:
        start collecting
        continue
    write the event to the sink
```

### Collecting a subtree

The **collector** decides when a subtree begins and ends. When the current path has
patches, a scalar is a complete subtree by itself. A `BeginObject` or `BeginArray`
starts a collection at depth 1. Each later begin event raises the depth and each end
event lowers it, and the subtree is complete when the depth returns to zero.

The collected events are then built into a tree. This is the only place in the fold
where a tree is built, and it is built for the patched subtree alone. The patches for
its path are applied to it in order, and the result is written to the sink as events.

Here is the fold of `{a: {x: 5}}` onto the base shown earlier. The root of the patch is
`a.x`.

| base event | path | what the fold does |
|---|---|---|
| `BeginObject` | (root) | passes it through |
| `Key("a")` | `a` | passes it through |
| `BeginObject` | `a` | passes it through |
| `Key("x")` | `a.x` | holds the key |
| `Int(1)` | `a.x` | collects it, patches it to 5, writes `Key("x")` and `Int(5)` |
| `Key("y")` | `a.y` | passes it through |
| `Int(2)` | `a.y` | passes it through |
| `EndObject` | `a` | passes it through |
| `Key("b")`, `Int(3)` | `b` | passes them through |
| `EndObject` | (root) | passes it through |

One scalar was materialized. Had the document been a hundred megabytes, the same one
scalar would have been.

### The held key

A key whose value is about to be patched is not written when it is read. It is held
until the patched value is known.

The reason is deletion. The patches may resolve the value to nothing, and in that case
the key must not appear in the output either. Absence is written by writing nothing.
Folding `{a: {x: !delete null}}` onto the same base gives

```
BeginObject  Key("a")  BeginObject  Key("y")  Int(2)  EndObject  Key("b")  Int(3)  EndObject
```

in which neither `Key("x")` nor a value for it appears. An element of an array has no
key, so deleting one writes nothing for it, and the elements after it shift down. That
is what deleting an element of an array means.

### Comments

A head comment is an event which comes before the value it describes. If the value is at
a patched path, the comment has to be collected with it, so that the value is patched
together with its comment and written back together with it.

The collector therefore holds a comment which arrives at a patched path, and waits for
the value. If the value comes, the comment becomes the first event of the collected
subtree. If it does not, because a container closed or another key began, the comment
described nothing that was collected, and it is handed back to the stream.

Exactly one of the collector and the stream owns each event. An earlier version took the
comment *as* the value. The patch was then applied to a comment, the real value stayed
in the stream, and the object was written with the same key twice.

## Writes the base never reaches

The fold as described so far applies a patch when the base stream arrives at the patch's
path. A write which *creates* structure has no such path. If the base is `{a: 1, b: 2}`
and the write is

```tony
{c: {d: 1}}
```

then the stream never arrives at `c.d`, and the patch would never be applied.

This matters more than it first appears to. The next snapshot is built by the same fold,
so a write which the fold drops is missing from every later snapshot as well.

The fold therefore keeps the set of patch paths which have not yet been reached. It
follows the shape of the base with a stack of open containers, and it places each
unreached patch at the deepest point of the base which does exist. This is called
**grafting**. There are three places where a graft can be made.

### As a new key in an existing object

This is the usual case. The write above is grafted into the root object as a key `c`,
whose value is `{d: 1}`.

The key must be written where it *sorts*, and not at the end of the object. Storage
keeps the keys of an object in sorted order, and since snapshots are built by this fold,
an unsorted result would become an unsorted snapshot. So when a key of the base arrives,
every pending graft whose key sorts before it is written first. Whatever is still
pending when the object closes is written just before its end event.

```tony
# base {a: 1, c: 2}, write {b: {d: 1}}
{a: 1, b: {d: 1}, c: 2}

# base {a: 1, b: 2}, write {c: {d: 1}}
{a: 1, b: 2, c: {d: 1}}
```

A base written before storage kept keys sorted may hold an object whose keys are out of
order. The fold notices this as the keys arrive, and for that object it grafts at the
close instead.

Two pending paths which share a first missing key, such as `c.d` and `c.e`, are grouped
and written as one key with a merged value, and not as the same key twice.

### In place of a scalar

If the base holds a scalar where the write needs a container, the scalar is replaced
rather than followed by a second key:

```tony
# base {a: 1, b: 2}, write {a: {c: 1}}
{a: {c: 1}, b: 2}
```

### In place of an array

If the base holds an array at a path, and a write names a *field* beneath that path, the
array is replaced by an object when its opening event arrives, and its events are
skipped through to its close:

```tony
# base {a: [1, 2]}, write {a: {d: {e: 1}}}
{a: {d: {e: 1}}}
```

The replacement must be made at the opening event. By the time the array closes, its
elements have already been written to the sink.

### What cannot be grafted

A missing position in an array, or a missing element of a keyed array, cannot be
created by the fold. The elements of the array have already streamed past, and for a
keyed array the field which names an element is in the schema, which the fold does not
have. Such a path is an error. It is not silently dropped, since a silently dropped
write is the defect that grafting exists to prevent.

### Wrapping, not folding first

When several patches are grafted beneath one missing key, each is wrapped in the objects
which the missing path names, and the wrapped patches are applied in order. They are not
applied to one another first and the single result wrapped.

The difference shows when a write is later deleted:

```tony
# base {a: 1}
{b: {c: 1}}
{b: {c: !delete null}}
# result
{a: 1, b: {}}
```

The delete removes `c`, and not the `b` which the earlier write created. Folding the two
patches together first would collapse them to nothing, and `b` would be missing.

There is a second reason. Applying a patch consumes the operations in it. `!raw` is an
operation whose purpose is to be consumed: it installs its subtree as data, with the
tags in it intact. If two patches were applied to each other in advance, a document
stored under `!raw` would come out of that step with its operators no longer escaped,
and the next application would run them. A patch is applied once, by whatever is
accumulating the state. What is wrapped here is still a patch.

## An empty base

A path which has no snapshot beneath it has no base events. There is then nothing to
stream the patches into. The patches are applied in order starting from null, and the
result is written as events. If everything is deleted, nothing is written, which is the
representation of the absent state.

A delete followed by a write in the same list is handled the same way at any path: the
state after the delete is treated as null, so the later write rebuilds from null.

## What the fold may assume

The fold has no branch for a patch which does not apply. It does not need one, because
of two rules that are enforced when a write is accepted.

- Every patch is applied, before it is stored, to the state it would be stored against.
  A patch which does not apply is refused while the client is still waiting for the
  answer.
- An operation whose meaning depends on what it lands on, such as `!replace` or
  `!arraydiff`, is not stored as written. It is applied, and its result is stored in its
  place. This is called **lowering**. What the log keeps is only operations which state
  what a value *is*.

Together these make replay total: a stored patch applied again to its base gives what it
gave at the write. They are described in
[What a write must be](https://signadot.github.io/tony-format/logd/writes/).

## The fold as a cursor

For a read, the fold runs beside the reader. It writes each event into a channel with
room for 64 events, and the cursor's `Next` takes events from the channel. The capacity
of the channel is all that the fold holds ahead of the reader. If the cursor is closed
early, the fold stops at its next write.

A cursor also answers a question before its first event: whether the path is absent,
holds a null, or holds a value. A read at a path which the index can show was never
written answers "absent" without opening any file.

The same fold builds the next snapshot. In that case the base is the previous
snapshot's events, the patches are the writes since, and the sink is the snapshot
builder. Since a read and a snapshot are produced by one piece of code, they cannot
disagree about what a patch does.

## What it costs

The fold costs the sum of three things.

- **The base events streamed.** A read at a path opens the snapshot at that path through
  the snapshot's own index, so this is the size of the subtree and not of the document.
- **The subtrees materialized.** Each maximal path is built as a tree once.
- **The patches held.** These are the projected writes since the snapshot.

Two of these have a worst case which is worth knowing.

A patched subtree is materialized whole. A patch whose root is a large container
therefore costs the container. A write beneath it does not, since its root is deeper.

A record is decoded whole before it is projected. If one write installs a container with
ten thousand children, a read of any one child decodes the whole write in order to
project the child from it. This was measured at 44 ms for each such read. The number of
writes folded is one in that case, so a policy which counts writes does not see it. logd
handles it separately: a read which decoded a record much larger than what it answered
schedules a snapshot of the parent of its path, so that later reads beside it seek into
the snapshot instead.

This article quotes no figure for the common case, a change to one field of a large
document. The package has benchmarks for it, and a figure should be taken from a run of
them before this is published.

## Limits

- The fold cannot create a position in an array or an element of a keyed array.
- A patched subtree is built as a tree, however large it is.
- A commit of one field into a large set still decodes the record which wrote the whole
  set. This is an open issue (`git issue show eztp171x`).

## Summary

- A patch carries no marker of where it applies. Its roots are read from its shape.
- Each write is projected onto the path of the read before it is held, by one rule which
  the watch and docd also use.
- Nested roots are reduced to maximal paths, and every patch contributes at the maximal
  path in commit order. Nothing is dropped.
- The base is streamed. Only the subtree at a maximal path is built as a tree.
- A key is held until its value is known, so that a delete removes the key.
- A write to a path that the base does not have is grafted where its key sorts.
- The fold assumes that every stored patch applies, and the write path guarantees it.

---

*logd and the Tony format are open source at
[github.com/signadot/tony-format](https://github.com/signadot/tony-format). The code
described here is in `go-tony/system/logd/storage/internal/patches` and
`go-tony/system/logd/storage/cursor.go`. Issues are kept in the repository itself and
are listed with `git issue list`.*
