# The read and write interface

Prerequisite 1 of wk5w1ddkh12krj1tkxn0: the interface stated in terms of a cursor over the
event store plus an in-memory delta, with no total-materialization entry point to fall back
to. It assumes element identity (element_identity.md), because a cursor seeks to a NAME.

The whole surface:

    Read(at Commit, view View, kp KPath)    (Cursor, error)
    Deltas(from Commit, view View, kp KPath) (DeltaCursor, error)

    Begin(view View)                        (*Tx, error)
    (*Tx) Write(kp KPath, delta *ir.Node)   error
    (*Tx) Require(kp KPath, want Precondition) error
    (*Tx) Commit()                          (Commit, error)

and two wrappers over a cursor, `Rooted` and `Collect`, which are the whole of what used to
be five entry points and nine ways to read.

## Two of the three axes stop being axes

read.go says every read varies on VIEW, EXTENT and SOURCE, and that a caller reached for one
and paid for another three times in a month (ap8ddvp2h12krd43gdn0, kds4sx3bh12krdrkghn0,
ntadpaech12krandgsn0). The reduction is not that those reads were wrong. It is that two of
the axes are not the caller's to vary.

EXTENT IS NOT AN AXIS. There is no whole-document read to be the other value of it. A read is
at a path, and the root is a path, so `Read(at, view, "")` is a root cursor and not a
different function. This is the property the issue insists on: if a wide read is left in the
interface, every path finds it again.

SOURCE IS NOT AN AXIS. Replayed or stepped is a question about where it is cheapest to seek
FROM, which the store can answer and a caller cannot. A cursor decides it from the index --
the nearest snapshot at or above kp, at or below `at` -- and nothing in the signature lets a
caller choose wrong. The three performance bugs were the axis existing, not the choices being
hard.

VIEW SURVIVES, as a parameter and never as a function. A scope is baseline with the scope's
own writes folded last; that is one more source in the same fold, which is what
4wpqh7t2h12ks1fvj5n0 means by scopes stopping being a special case.

## What a cursor is, and why it is bounded

    type Cursor interface {
        Presence() Presence               // before any event, and Absent means none follow
        Next() (*stream.Event, error)     // io.EOF at the end
        Close() error
    }

Events, in document order, for the subtree at kp -- the substrate's own currency, which is
what makes this an interface over the event store rather than one on top of it.

A read is three steps, and the bound is a property of each:

  1. SEEK. From the index, the nearest snapshot at or below `at`, opened at kp through the
     snapshot's own path index, and a SEGMENT CURSOR over the segments in that range which
     touch kp. Not `[]LogSegment`: that signature returned 166 MB before a single log entry
     was read.
  2. PROJECT AND COMPOSE. Each patch the cursor yields is projected to kp as it is read, and
     composed into one running delta. Not accumulated: `patchNodesFromSegments([]LogSegment)
     ([]*ir.Node, error)` held 511 MB because it kept every patch in the range, whole and
     unprojected, at once.

     THE CURSOR'S LENGTH IS THE NUMBER OF WRITES TO KP, NOT THE NUMBER OF COMMITS. The index
     is asked for the segments that touch kp, and the `Spine` flag is what lets it skip the
     writes to kp's siblings -- a patch that only passed THROUGH kp's ancestors on its way
     elsewhere is described by the segments deeper down and does not concern this read. A
     thousand commits between the snapshot and `at` are a thousand different paths, and a
     read at one of them steps over almost all of them without reading a record. The count
     could be a million and the read would be the same size.
  3. FOLD. One streaming pass: base events from the snapshot at kp, the composed delta
     applied over them, events out.

So the resident terms are the composed delta and one record, and the composed delta is
bounded by WHAT CHANGED UNDER KP SINCE THE SNAPSHOT. That is the case the issue's bound
explicitly admits -- "if an intermediate applied state since the last snapshot is larger than
the final answer, that intermediate is itself a result size and is what the bound is stated
against". It is not a count of how much has happened under the path.

THE SEEK IS ALREADY BOUNDED, AND NOT BY PROPERTY 5. A snapshot is a stream of
tree-traversal events with a size-bound path index of `{Path, Offset}` beside it
(snap/index.go), so `ReadPathEventReader(kp)` seeks to kp's offset -- or to the nearest
indexed ancestor and streams forward from there -- and reads kp's subtree and no more. A
snapshot AT THE ROOT serves a bounded base read at any path in it. This is the substrate
doing exactly what it was built to do, and the interface inherits it rather than needing
anything new.

So what property 5 changes here is not memory but TIME, and less of it than a count of
commits suggests. Step 2 reads the writes to kp since the snapshot, not the commits since
it, so the distance to the snapshot is irrelevant to a path nobody has written. Where it
bites is the paths that ARE written: two commits in three leave a segment on the root and on
`verse`, so those are the reads a nearer snapshot shortens, and they are also the ones a
size-bound index has the most trouble describing. Prerequisite 1 is within its bound on a
store whose snapshots are all at the root, and fast there for everything but the shallow hot
paths. The two are worth doing together; they can be sequenced apart, and this document
should not claim otherwise.

A CURSOR HOLDS NO LOCK BETWEEN CALLS TO NEXT. It pins `at`, so no concurrent write disturbs
it and none waits for it; a commit paid 5.8s in its index phase because a walk held the
root's read lock for the length of the walk (kds4sx3bh12krdrkghn0). It pins the log
generation too, and if compaction moves the ground beneath it the cursor fails retryably
rather than reading what is no longer there.

## Presence comes before events

`Presence()` answers before the first event, and Absent means no events follow. That is
prerequisite 2 landing on this interface: absent is not null, structurally, so the watch gate
that collapses them cannot be written. `AbsentSpineAt` -- "no read at all: the index proves
the path was never written" -- stops being an entry point and becomes what a cursor answers
when the index proves it, without opening the log.

## The two wrappers, and the one that costs

    Rooted(c Cursor, kp KPath) Cursor     // emits kp's ancestors as opens, then c
    Collect(c Cursor, budget Bytes) (*ir.Node, error)

`Rooted` is what `ReadSubtreeRootedAt` was, done as a wrapper instead of a re-rooting of a
built node -- which is why it works through a non-field segment where the old one declined
(thqtmm2th12kr051jhn0's third defect). It emits ancestor opens and then the subtree's events.
Nothing is held.

`Collect` is the only materialization in the interface and IT TAKES A BUDGET, with no
default. A caller that wants a node says how large a node it is prepared to hold, and is
refused past it. This is not a wide read wearing a hat: a wide read is one where the caller
does not know or say what it is asking for, and every call site here states a number that a
reviewer can read. The nine reads existed because there was always a total read to retreat
to; there is no retreat here, only a caller declaring its own budget.

## Writing

    tx := store.Begin(view)
    tx.Write(kp, delta)
    tx.Require(kp, want)
    commit, err := tx.Commit()

A WRITE IS A RECORD AT A PATH, NOT A DOCUMENT APPLIED TO A DOCUMENT. `steppedStateAt` -- "the
state a write at the next commit is applied to" -- has no successor here, because nothing is
applied to anything at write time. The delta is the client's, in memory, bounded by the
request; lowering names the paths it touches; the record is stored at those names.

`Require` is the precondition, and it is a bounded read at the path it names, not a
navigation down a materialized head. `commitOps.MatchStateAt` answered every precondition
from the whole head after navigating down; a precondition at `items(sku=A).qty` reads
`items(sku=A).qty` and nothing else.

## The head is a number

Not a document. The head is the current commit, and every question about current state is
`Read(head, ...)`. This is rkb7p8v5h12ksdnmgsn0's answer taken all the way: not a smaller
kept document, no kept document.

It also retires a check rather than replacing it. `CheckHead` exists because the stepped head
is a second computation of the same state and could drift, so a full read at every snapshot
compared them. With no second computation there is nothing to drift, and the full read that
compared them was itself the last routine caller of a total materialization.

## One delta shape

    Deltas(from, view, kp) DeltaCursor

Live and replay are the same function. A commit's notification is the stored, lowered record
projected to kp -- the same object a replay reads, not a second delta built from the client's
merged patch (4wpqh7t2h12ks1fvj5n0). A watch is `Read(at, view, kp)` for where to start and
then `Deltas(at+1, view, kp)`, both rooted at the watched path, which is
rg5nd1psh12kse7dddn0 answered by there being one rooting rather than two.

## What holds the property

A signature rule, checkable in review and worth stating because this is the third round of
locally-right work on the same shape:

    No exported function returns a slice, map, or node whose length grows with the number of
    commits, segments, or elements in range.

Both large terms of the read caught in flight violate exactly that and nothing subtler.

And the bound is reported, per read, in the terms it is stated in: bytes emitted, largest
record buffered, and whether the seek found a snapshot at or above the path. A bound that is
not measured is a comment. The keyed-read counters (c3e53a2) become this, in one place
instead of per-variant.

NO STRANGLER. The old entry points are deleted, not deprecated. The property is enforced by
the ABSENCE of a total read, and the two thirds of the tests that drive Open / NewTx / Commit
and assert outcomes are what hold the rebuilt interface to the same answers.

## The nine, mapped

    ReadStateAt                 Read(at, view, kp) -- and the callers' trimming goes with it
    ReadSubtreeAt               Read(at, view, kp)
    ReadSubtreeRootedAt         Rooted(Read(at, view, kp), kp)
    AbsentSpineAt               Presence() == Absent, proven from the index
    steppedStateAt              nothing: a write is not applied to a document
    replayBaselineAt            step 1's seek, when the snapshot is far
    steppedBaselineAt           step 1's seek, when it is near
    replayScopedAt              the same, with view
    steppedScopedAt             the same, with view
    narrowSubtreeAt             the cursor itself

## What is deliberately not decided

  - `Precondition`'s vocabulary. It is a bounded read plus a predicate; which predicates
    (presence, equality, a match) is a question for the write path, and 4wpqh7t2h12ks1fvj5n0
    asks the same question of the delta.
  - Whether `Collect`'s budget is bytes, events, or both. Bytes is written here because it
    is the term the bound is stated in.
  - Back-pressure and cancellation on a long cursor, which is a server concern rather than a
    storage one, and belongs with the session protocol.
