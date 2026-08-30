# logd, mergeop: a single-key operation on a keyed array costs the whole array

A keyed array exists so an element can be named by identity instead of position. The
STORED delta already honours that -- a write naming one element stores one element -- but
every other stage still touches the whole array, and a read at an element touches the
whole DOCUMENT. This is the plan for closing that, in stages that are each useful alone.

## Where the cost is paid today

    stage                     cost of a single-key operation
    ---------------------------------------------------------------------------
    stored delta              O(elements written)      already right
    live index                O(1) added, O(N) resident  a segment per element
    merge / apply             O(N), and deep-CLONES N  mergeop/keyed_list.go:61
    read at an element        O(whole document)        falls back to the wide read
    snapshot seek             not addressable at all   the index is positional

Measured, patching ONE element of an N-element keyed array through tony.Patch:

    N=100     316us
    N=1000    1.155ms
    N=10000   9.548ms
    N=40000   44.71ms

Linear in N at ~1.1us per element, for a patch that names one. The cause is not the scan
but the copy:

    dst := make([]*ir.Node, len(doc.Values))
    for i := range doc.Values {
        dst[i] = doc.Values[i].Clone()      // Clone -> CloneTo, recursive
    }

every element deep-copied before the key map is consulted.

## What already streams, so this is shorter than it looks

  - the dlog is length-prefixed and read by structure, never materialized whole;
  - a snapshot is an event stream with a chunked offset index (4096 bytes per entry,
    snap/constants.go) and a streaming PathEventReader -- so a seek is already O(chunk);
  - a POSITIONAL element read already works at the storage layer. Verified against a
    store with a snapshot: ReadSubtreeAt(`items[2]`) narrows and answers `{q: 7 sku: G}`
    without the array. It does NOT reach a client -- see fence 4, which discards it.

So the machinery for "seek to an element and stream it" exists. What is missing is that
none of it can be addressed BY KEY.

## The five fences, in the order they bite

Verified by probe against a store with a snapshot and a keyed array of three elements.

1. **The snapshot index is positional and schema-blind.** Its paths come from
   `stream.State.CurrentPath()`, whose own doc says "the current kinded path (e.g. "",
   "key", "key[0]")". A stream carries no schema and does not read the array's `!key`
   tag, so an element is indexed as `items[2]`, never `items("G")`.

   Consequence, measured: at a commit where the snapshot holds the element and no patch
   is above it, `ReadSubtreeAt('items("G")')` answers **(nil, narrowed=true)** -- for an
   element that exists. Counted as reads.wide.absent. Its one caller reads a nil node as
   "go wide" and is therefore correct, but the pair it returns says "narrowed, and there
   is nothing there", which is false. A second caller trusting it would be silently wrong.

2. **projectPatchesAt will not descend the `!key` operator.** With a patch above the path,
   `ReadSubtreeAt('items("G")')` declines with reads.wide.operator -- the stored delta is
   `{items: !key(sku) [...]}` and the projection stops at the operator. This is the fence
   that actually fires in ordinary operation, and it fires for `items[2].q` too.

3. **The merge clones the array**, above.

4. **ReadSubtreeRootedAt cannot re-root through a non-field segment**
   (read_subtree.go): `kpath.SegmentFieldName` reports `items("G")` is not a field, so it
   answers not-narrowed and the caller reads wide.

   This is the fence that matters most and I under-rated it when this was filed. It is not
   keyed-specific: `items[2]` narrows at the storage layer and returns the element, and
   this throws it away, so no read at an element of ANY array reaches a client narrowed.
   Stage 2 is therefore worth doing for positional paths whether or not keying is ever
   addressed. Until stage 0 it also did the discarded read first, paying for the narrow
   read and the wide one.

5. **RootPatchAt cannot express an element path at all.** `items("A")` carries the key
   VALUE where building the structure needs the key FIELD, which is why RootKeyedListAt
   exists: root at the ARRAY, carry a one-element `!key(f)` list. The write side already
   has this workaround; the read side does not.

## An incremental path

Each stage stands alone and is measurable by the read counters.

**0. Make the counters tell the truth (no format change). DONE, c3e53a2.** A keyed path
that goes wide is counted as keyed rather than as "operator" or "absent", so the later
stages have a number to move: a store doing nothing but keyed reads used to report
reads.wide.keyed-or-idx = 0. Fence 1's return went with it -- a keyed path the narrow read
cannot address now declines instead of answering "narrowed, absent", which was a wrong
answer that only ReadSubtreeRootedAt's convention of reading nil as "go wide" kept from
being seen. And ReadSubtreeRootedAt now decides from the path before reading, so the
discarded-then-repeated read in fence 4 is gone.

**1. Project through `!key` (no format change).** `projectPatchesAt`, meeting `!key(f)`
with a `(v)` segment next, selects the element whose f is v and projects to it. This is
the same identity the merge already implements, applied to the patch rather than the
document, and it removes fence 2 -- the delta range then costs the element. The base still
costs whatever fence 1 leaves.

**2. Re-root through a keyed segment (no format change, needs the schema).**
`ReadSubtreeRootedAt` wraps its answer in a `!key(f)` single-element list rather than
giving up -- the RootKeyedListAt construction, in reverse. The key field comes from
`schemaForScope`. Removes fence 4. Worth keeping the doc comment's caution: the wide read
distinguishes absent from ancestor-is-a-scalar from empty-document, and this must not
answer those.

**3. Stop cloning the array (no format change).** Two independent halves of fence 3:
build the document's key->position map once instead of rescanning, and clone only the
elements the patch names, sharing the rest. Both are local to keyed_list.go. This is the
largest immediate win and the only stage that needs nothing from anyone: on the numbers
above it takes a single-key write on 40k elements from 45ms to roughly the cost of one.

**4. Index keyed elements in the snapshot (format change, compatible).** The builder
learns the key field for an array -- from the schema, at snapshot time -- and records
`items("G")` where it records `items[2]` today. The stream state has to carry the key for
the array it is inside, which is where the schema-blindness has to end. Compatible in both
directions: the index is chunk-granular and the reader already falls back to the nearest
ancestor, so an old snapshot degrades to today's behaviour rather than breaking. This is
the stage that makes a single-key READ cost a chunk instead of a document, and it is the
prerequisite for the rest being worth having.

**5. The live index, which we are not ready for.** `Index.Add` builds a child Index per
path segment, so a keyed array of N elements is N resident subtrees -- addressable in O(1)
and resident in O(N). Making a single-key operation out of memory END to end needs the
index itself to be traversable on disk, which is a larger change than any of the above and
should not be smuggled into one of them. Stages 0-4 are all worth doing before it, and
none of them is invalidated by it: they make the DELTA and SNAPSHOT layers key-addressable,
which is what an out-of-memory index would then be indexing.

## Note

Stage 3 alone changes the shape of writes, and stages 1+2+4 together change the shape of
reads. Nothing here requires the vocabulary to grow: `!key` already means identity, and
RootKeyedListAt already shows how an element is named in a patch. What is missing is that
the read path, the projection and the snapshot index never learned it.

The one thing that DOES need vocabulary is absence -- stating that an element is not in a
keyed list. That was the third reason the scope overlay could not be derived
(qth3kqe9h12ksxz9j9n0) and it is unresolved; it is not on this path, but a delete of a
keyed element will meet it.