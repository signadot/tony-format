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

## The work, by what it unblocks

Numbering these 0..5 was a mistake in the first draft: it read as a sequence, and they are
not one. There is exactly ONE ordering constraint in the whole plan, and the rest is a
choice about which cost to stop paying first. Grouped by what each actually finishes.

### Measurement -- DONE (c3e53a2)

Counters that name a keyed read as keyed rather than as "operator" or "absent", so
everything below has a number to move. A keyed path the narrow read cannot address now
declines instead of answering "narrowed, absent", and ReadSubtreeRootedAt decides from the
path before reading rather than discarding a read it just did.

### Stands entirely alone: the write path

**Stop cloning the array** (mergeop/keyed_list.go). Build the document's key->position map
once instead of rescanning, and clone only the elements the patch names, sharing the rest.
Depends on nothing here, is depended on by nothing here, needs no format change and no
schema. On the numbers above it takes a single-key write on 40k elements from 45ms to
roughly the cost of one element. Best ratio in the plan and it can be done today.

### Finishes positional reads by itself: the read API

**Re-root through a non-field segment** (ReadSubtreeRootedAt). `items[2]` already narrows
at the storage layer and returns the element; nothing can deliver it. Doing this alone
makes every POSITIONAL element read narrow end to end -- no keying involved, no format
change. For a KEYED path it also needs the two below, because the read declines earlier.
The key field for the `!key(f)` wrapper comes from schemaForScope.

### Finishes keyed reads, and only together: delta + snapshot

Neither of these completes a keyed read alone -- with either missing, the read still
declines at the other -- and both need the re-rooting above to deliver the result. This is
the one place in the plan where order matters, and even here the order is "all three
before any keyed read narrows", not a sequence.

**Project through `!key`** (projectPatchesAt). Meeting `!key(f)` with a `(v)` segment next,
select the element whose f is v and project to it -- the identity the merge already
implements, applied to the patch. Covers the delta range. No format change.

**Index keyed elements in the snapshot** (snap builder, format change, compatible). The
builder learns the key field for an array from the schema at snapshot time and records
`items("G")` where it records `items[2]`. The stream state has to carry the key for the
array it is inside, which is where the schema-blindness ends. Compatible both ways: the
index is chunk-granular and the reader already falls back to the nearest ancestor, so an
old snapshot degrades to today's behaviour. Covers the base.

### A different axis: residency, which we are not ready for

**The live index out of memory.** `Index.Add` builds a child Index per path segment, so a
keyed array of N elements is N resident subtrees -- addressable in O(1), resident in O(N).
This is not the next stage of the above; it is the other half of "out of memory", and the
work above is about ADDRESSABILITY while this is about what has to be held. Everything
above is worth doing before it and none of it is invalidated by it: they make the delta and
snapshot layers key-addressable, which is what an out-of-memory index would be indexing.

### If you want one order

The clone fix, because it is free of everyone else and buys the most. Then re-rooting,
which finishes positional reads on its own. Then projection and the snapshot index
together, which is when a keyed read finally narrows. Residency last, on its own terms.

## Note

Stage 3 alone changes the shape of writes, and stages 1+2+4 together change the shape of
reads. Nothing here requires the vocabulary to grow: `!key` already means identity, and
RootKeyedListAt already shows how an element is named in a patch. What is missing is that
the read path, the projection and the snapshot index never learned it.

The one thing that DOES need vocabulary is absence -- stating that an element is not in a
keyed list. That was the third reason the scope overlay could not be derived
(qth3kqe9h12ksxz9j9n0) and it is unresolved; it is not on this path, but a delete of a
keyed element will meet it.