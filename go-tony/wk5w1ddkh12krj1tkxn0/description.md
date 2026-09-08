# logd: out-of-memory by construction survived in the event substrate and was lost in the API above it -- rebuild above that line

Out-of-memory by construction was always the goal, in the usable form of it: not streaming
the session protocol as events, which nothing could consume, but every READ and WRITE
implemented against the event store with a working set bounded by the delta and the path.
An in-memory ir.Node for a delta, or for a bounded answer, is consistent with that and
always was. The store is an indexed store of tree-traversal events; the diff is the thing
in memory.

That property did not decay evenly. It survived below a line and was lost above it, and the
line is worth naming because it is also the line this proposal keeps.

WHERE IT SURVIVED. internal/snap, internal/patches, internal/dlog, index -- the substrate.
snap indexes traversal events by path and ReadPathEventReader seeks to an offset and streams
from it (snap.go, path_finder.go); patches applies a delta through a streaming processor;
applyPatchesToBase folds projected patches onto streamed base events. A subtree read touches
the subtree. This layer still does what it was designed to do, and storage/CLAUDE.md still
states the rule it was designed to: "containers MUST be treated out-of-memory for subpaths".

WHERE IT WAS LOST. Everything above the substrate.

  - ReadStateAt materializes the whole document, and says so about itself: "kp does NOT
    narrow the result ... what comes back is the document, root-rooted -- a SUPERSET of kp's
    subtree, which callers trim" (read.go). It is also what every other read falls back TO.
  - The head is one whole document (Storage.head), stepped per commit, and every CAS
    precondition is answered from it (commitOps.MatchStateAt) after navigating down.
  - Every baseline watcher kept another whole document and folded the same delta onto it,
    per commit, per watcher (rkb7p8v5h12ksdnmgsn0).
  - read.go's own table names NINE ways to read state, and records that three of one month's
    performance bugs were callers reaching for one and paying for another.

So the shape of the defect is: the substrate is bounded, the API above it is not, and the
API is what every caller touches. Nine reads exist because there was always a total read to
retreat to; a caller that could not narrow simply materialized the document instead.

WHY THIS IS NOT A LIST OF BUGS TO FIX. The last several rounds of work on this have each
been locally right and globally unbudgeted -- narrow reads, then a stepped head, then a
stepped watcher, then narrowing the projection. Each removed one materialization and left
the shape. The reason it does not converge is that two of the five properties below change
the SIGNATURES, and a signature cannot be fixed one caller at a time:

  a read that is out-of-memory by construction answers with a cursor and a bounded delta,
  not with a document; and an index that addresses an element by IDENTITY is not the index
  that addresses it by position.

THE FIVE PROPERTIES, AND WHERE EACH LANDS

  1. Out of memory by construction. Above the line. The substrate already provides it; the
     API has to stop offering a way around it.
  2. Null and absent structurally distinct, not merely behaviourally. Above the line, and
     entirely inside logd: absence is the (nil, nil) idiom today, and each layer re-decides
     what it means. lowering keeps the two apart on purpose -- "the absent document is not
     the null one" (lower.go, xqpvk3ehh12ks89mj5n0) -- while the watch gate collapses them,
     so a watched path holding null that is then deleted delivers no event.
  3. !logd-key / !logd-auto-id replacing positional keying, with a path forward for several
     keys on one array and for a key composed of several fields. Above the line, and the
     deepest of the five: the snapshot indexes an element by POSITION (stream.State.
     CurrentPath answers items[2], never items("G")), which is why a keyed read cannot
     narrow at all today (thqtmm2th12kr051jhn0).
  4. Snapshotting, logA/logB, compaction, lowering: preserved. This is the substrate plus
     lowering, and it is the part that kept property 1.
  5. Snapshots incorporated into compaction. Above the line, internal to storage.

Four of the five are above the line. The fifth says keep what is below it.

WHAT A REBUILD COSTS, AND WHAT IT KEEPS

    logd today            23,817 lines of code, 28,314 of tests, 516 test functions

    rebuilt above the line ~13,500   server 3,734, storage core 5,043, tx 2,055, index 2,715
    carried below it       ~5,100    dlog 2,191, snap 1,543, patches 1,374, plus lowering
    the format library     untouched ir/mergeop/libdiff/stream/parse, 13,679 lines, reached
                                     by 229 non-test files -- and 120 released versions

That last row is the reason this is a logd proposal and not a tony one. Nothing here needs a
change to the value model or a v2 of the library.

THE TESTS MOSTLY TRANSFER, which is what makes it affordable. Every logd test is in-package,
but only 21 of 61 storage test files and 6 of 21 server test files actually reach for
internals. The other two thirds drive Open / NewTx / Commit / ReadStateAt and sessions and
assert outcomes: they are a behavioural specification of the layer being rebuilt, and they
hold the rebuild to the same answers. The third that is white-box is rewritten or dropped.

WHAT HAS TO BE WRITTEN DOWN FIRST, or this is a second accretion rather than a design.

  1. THE READ AND WRITE INTERFACE, stated in terms of a cursor over the event store plus an
     in-memory delta, with NO total-materialization entry point to fall back to. A root read
     is then a cursor at the root, not a different function. If there is a wide read left in
     the interface, every path will find it again.

  2. PRESENCE AS A TYPE. A read answers "there is nothing here" as a value, not as a nil
     with a convention attached. Property 2 is not a behaviour to be tested for; it is a
     type that makes the two states unrepresentable as one.

  3. ELEMENT IDENTITY, from !logd-key and !logd-auto-id down through the snapshot's path
     state and the index. This is the one with real unknowns and it should be settled on
     paper: what a composite key and a multi-key array serialize to as a PATH SEGMENT, since
     that segment is what the index keys on and what a client addresses; and what happens to
     an element whose key changes. Positional addressing survives for arrays that declare no
     key, and stays the one thing an insert or delete must restate from the index up.

  4. ONE DELTA SHAPE. A commit's notification is built from the client's merged patch while
     a replay reads the STORED, lowered entry, so live and replay deliver different deltas
     for the same commit and only one of them is absolute. One shape, decided once
     (4wpqh7t2h12ks1fvj5n0 is the same question asked of the write path).

WHAT WOULD MAKE THIS FAIL

  - Doing it as a strangler with both APIs live. The property is enforced by the ABSENCE of
     a total read; keeping the old one during migration keeps the escape hatch that produced
     nine reads.
  - Losing the comment record. The reasons for the non-obvious choices are in the prose and
    the commit messages, keyed to issue ids, and they are the most valuable thing in the
    tree. A rebuilt file that drops the reason re-earns the bug.
  - Rebuilding the substrate. dlog's A/B switching, crash recovery and generation handling,
    and snap's format, are where the durability semantics live. Property 4 says keep them;
    keep them literally.

ISSUES THIS SUBSUMES OR ANSWERS

  rkb7p8v5h12ksdnmgsn0  the head and every watcher fold a whole document -- the head becomes
                        a cursor rather than a smaller cache
  thqtmm2th12kr051jhn0  a keyed element cannot be addressed -- property 3
  9b2vpggxh12ks0qde5n0  scoped reads cannot be stepped
  sb33w8p9h12kr16kg5n0  a scoped write rebuilds its view
  rg5nd1psh12kse7dddn0  a watch's state and patch events are rooted differently
  4wpqh7t2h12ks1fvj5n0  one storable-delta pipeline -- property 4
  qvn7ptxch12krxzt9hmg  index size bound

Related and NOT subsumed, because it is in the value model rather than in logd:
mgg9nvt6h12krn6dksn0 (!raw conflates escaping with replacing).