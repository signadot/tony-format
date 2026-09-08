# Index residency

The fifth prerequisite of wk5w1ddkh12krj1tkxn0, and its sixth property. Neither number is in
that issue's description, which names five properties and four prerequisites; both are
settled in its discussion, and this document stands on the same footing as the other four.

The index has no bound. Measured on verse staging: 1.12 GB resident, 85% of a 1.31 GB live
floor, ~266 bytes of B-tree leaf per segment across 4,519,816 segments. That is the store's
memory problem, and none of the five properties in wk5w1ddkh12krj1tkxn0 covers it --
property 1 bounds a read's working set, which is the transient, while this is the floor.

Which policy bounds it matters less than that there is ONE PLACE that decides and that the
decision is ENFORCED. This document is the place and the enforcement; LRU is the starting
policy because it is the one that needs no evidence to justify.

## The inversion that has to happen first, and it is not the policy

    Today the index IS the truth, and index.gob is a copy of it kept for restart.
    For any of this to be safe, the durable form must be the truth and the resident
    form a CACHE of it.

Eviction is only correct if what is evicted can be got back, and got back without changing
an answer. So the durable index must be complete and seekable by path -- which is what the
persisted form already holds, 170 MB of it -- and the resident structure becomes derived.
Nothing else in this document is hard; this is.

The invariant that follows is the one to enforce, because it is what makes any policy safe:

    EVICTION CHANGES A COST, NEVER AN ANSWER. A read that misses pages in. It never
    sees a shorter segment list, an absent path that exists, or a path that does not.

## The unit is a region, not a node

A trie node holds a path's whole history. Its recent tail is what reads ask for and its
depths are what they do not, so evicting whole nodes evicts the wrong thing and keeps the
wrong thing.

    A REGION is (trie node, commit range). It is what is admitted, counted, and evicted.

This is also what makes the two skewed axes addressable at all. The path axis is measured --
ten paths carry 30.1% of 4.5M segments -- and the commit axis is assumed and unmeasured. A
region carries both, so a policy can be changed later without changing what it operates on.

## The seam

    type Residency interface {
        Admit(r Region, bytes int64) error   // refuses or evicts to fit; never grows past the ceiling
        Touch(r Region)                      // a read or write reached it
        Evict() []Region                     // what to drop next, policy's own order
    }

One implementation to start, LRU over regions, and the ordering is the only thing a later
frecency changes. The ceiling is configured, not emergent; the floor is what a single read
needs resident to make progress, and a ceiling below it is refused at startup rather than
discovered as thrashing.

A write touches the region it appends to, which makes what is being written hot without a
rule saying so.

## Enforcement, which is the point

A ceiling that is not enforced is a comment, and this one has to be enforced in the one
place regions are admitted -- not asserted afterwards by a sweep, because a sweep runs after
the memory is already held.

    - Every resident region is admitted through Admit and counted there. There is no other
      way to make something resident.
    - Resident bytes, the ceiling, the hit rate, and evictions per interval are reported.
      The 1.12 GB was found by reading a heap profile after the fact; a store should be able
      to say it.
    - The gradient is stated rather than arrived at: a read at the head hits, a read deep in
      history may pay I/O to the durable index. That is the intended shape and not a
      degradation to apologise for.

## Where this meets the rest

ELEMENT IDENTITY MAKES IT PRESSING. Naming elements multiplies index nodes, and a node is
not a map entry -- measured, an empty one is about four kilobytes, because every node carries
a B-tree that preallocates a 32-slot leaf of 120-byte segments. A keyed array of 100,000
elements with 200 hot ones is exactly the case a residency policy answers and a size bound
cannot. The rebuilt index also owes a node whose fixed cost is proportionate to what it
holds; that is a separate fix from this one and neither substitutes for the other.

COMPACTION IS COMPLEMENTARY. Property 5 shrinks what there is to describe -- 4,519,816 ->
47,648 measured as headroom -- and residency bounds what is held of it. Neither alone is the
answer: compaction cannot bound a live store between compactions, and residency cannot
recover the space history occupies durably.

AND THE BOUND ON CONTENT IS NOT THE ANSWER. An index truncated to live state and recent
commits would be small and would make history unusably slow by construction. An event store
whose index stops at the present has stopped being one. The bound is on residency; the index
still describes everything.

## What is deliberately not decided

  - Frecency's weighting, if LRU's measured hit rate turns out not to be good enough. The
    seam is what makes that a one-file change, and the hit rate is what would justify it.
  - Whether the durable index is the current persisted form paged, or a form designed to be
    paged. The first is the cheaper start and the measurement says whether it holds.
  - Whether the trie skeleton is itself evictable or is the floor. Simplest is the floor;
    that is an answer to be measured rather than argued.

## As built

THE INVERSION. The durable index is two files beside the logs: index.regions, append-only,
a framed record per region holding that region's segments; and index.manifest, small and
written whole, saying which records are current with each region's header -- its commit
range, its count, its snapshots -- so a store opens with the skeleton and every header and
pages nothing in to do so (index/regions_file.go). The resident trie is a cache of it: the
skeleton is the floor, and each node's segments are held in REGIONS, (node, commit range)
slices of at most 64 segments contiguous in StartCommit, which is the unit admitted,
counted, touched and evicted (index/region.go). Neither file is the record; the log is,
and a missing, torn, differently-versioned or generation-mismatched pair sends the store
back to it (index.OpenIndex says why; storage.init rebuilds).

THE INVARIANT holds by what may be evicted: only a region that is durable and clean --
whose record holds exactly what the tree does. A hot region, written to since the
persister last wrote it or never written, stays until it is persisted; the persister is
what turns hot regions into evictable ones, and Persist writes only what the file lacks.
A read pages the regions its range needs with no lock held and installs them under the
node's write lock in the same critical section that answers, so an eviction cannot come
between; a write inserts in that same section, because the charge for a page-in may evict
the region again when it is the only evictable one. A region admitted past the ceiling
with nothing evictable is admitted -- a read must be answered and a write must land --
and the excess is reported as index.over. What is never paged to say: the newest commit
at a node, the seek's target (each region carries its snapshots' commits), and the store's
watermarks, which all come from headers.

THE POLICY is LRU over regions; the ceiling is configured (storage.SetIndexCeiling,
indexCeiling in the file config), zero is unbounded, and a ceiling under the floor of
eight regions is refused. Reported: index.resident.bytes, index.ceiling, index.evictable,
index.hits, index.misses, index.hit.rate, index.evictions, index.over.bytes.

MEASURED: the property test drives one index under a ceiling eight regions wide against
the same index unbounded and reopened from its files, and the storage differential drives
a store under that ceiling against one unbounded, every path at every commit, with
evictions and misses counted; the shaped store is served under a tenth of its index's
resident bytes. What was found on the way was older than residency: a B-tree emptied to
its last element kept an interior root with no children and silently refused every later
insert, and removing a child's last element was reported as no removal. Nothing had
emptied a tree before; eviction does it routinely.

NOT DECIDED HERE, still: frecency over LRU (the seam is the region and the list); whether
the skeleton itself should be evictable (it is the floor, and it is what UnwrittenBelow
proves absence from without a read); a rewrite of the regions file on a garbage threshold
rather than at close.
