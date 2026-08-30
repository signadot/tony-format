# logd: a keyed element cannot be addressed, so no operation can be confined to one

Filed as "a single-key operation costs the whole array", with a plan in six numbered
stages. The first piece of it (f5f04d4) settled the part that was really about keying and
showed that the framing was wrong, so this is re-scoped to what is actually true.

## What the first fix settled, and what it showed

A keyed merge deep-cloned every element before consulting the key map. It now carries the
elements it does not name across, which is what the object merge has always done. Patching
one element of N, best of 5:

    N        cloning     sharing     an OBJECT of N, one field patched
    100      83us        83us
    1000     722us       768us
    10000    12.755ms    4.083ms
    40000    35.41ms     18.18ms     15.27ms

The last column is the finding. An object of 40k fields with ONE field patched costs
15.27ms on the path that has always shared -- so the 18.18ms that remains is not keying's.
Merging into a container of N children is O(N) in this package whatever the container is:
objMergeFast already shares the document's field slice when the keys are kept, and still
allocates two slices of N, walks N fields and re-parents N values, because the result is a
new container node that has to list them.

So "a single-key operation costs the whole array" was true, and the part of it that keying
caused is fixed. What is left is that a single-key operation costs the whole CONTAINER, and
a write costs the whole DOCUMENT, and neither is about keys.

## The goal splits in two, and this issue is only one half

"A single-key operation out of memory" needs both of:

**Addressability -- naming one element and reading it without the rest.** This issue. It
is a keyed problem, nothing else has it, and it is unfinished.

**Residency -- not holding or folding what the operation does not name.** Not a keyed
problem at all. It is rkb7p8v5h12ksdnmgsn0 (a write folds the whole document to verify and
step the head, and every watcher folds it again per commit: 38us / 78us / 137us of apply at
200 / 1000 / 3000 entities, scaling with the set and not the patch) plus the container cost
above plus the live index, which holds a child Index per path segment and so keeps a keyed
array of N as N resident subtrees.

**They need each other, which is the point.** rkb7p8v5's fix is to stop keeping the head as
one document and keep it as the subtrees a patch names, "turning both costs into O(patch)".
That granularity can only go as deep as a path can NAME. Today a path stops at the array,
because nothing below it can be addressed -- so a subtree head would still fold the whole
array for a write to one element, and the container cost above is exactly what it would pay.
Addressability is what lets the subtree be the element.

And the reverse: addressability alone buys a narrow READ and nothing else. Writes still fold
the document, watchers still fold the document, the index still holds every element. This
issue on its own does not make a single-key operation out of memory and should not be read
as claiming to.

## What is left here, and it is one thing in three places

A keyed element cannot be named or found. Verified by probe against a store with a snapshot
and a keyed array:

1. **The snapshot indexes elements by POSITION.** Its paths come from
   `stream.State.CurrentPath()`, whose own doc says "the current kinded path (e.g. "",
   "key", "key[0]")" -- a stream carries no schema and does not read the array's `!key`
   tag. `items("G")` matches nothing there; `items[2]` matches and returns the element
   without the array.

2. **projectPatchesAt will not descend the `!key` operator.** With a patch above the path
   the projection stops there, so the delta range cannot be narrowed to an element either.

3. **ReadSubtreeRootedAt cannot re-root through a non-field segment.** This one is not
   keyed-specific and is the most valuable of the three: `items[2]` narrows at the storage
   layer and returns the element, and re-rooting throws it away, so no read at an element
   of ANY array reaches a client narrowed.

Underneath all three: `RootPatchAt` cannot express an element path, because `items("A")`
carries the key VALUE where building the structure needs the key FIELD. `RootKeyedListAt`
is the write side's workaround -- root at the array, carry a one-element `!key(f)` list --
and the read side has no equivalent.

## The work

Ordering: only the last group has a constraint, and it is "all of it before a keyed read
narrows" rather than a sequence.

**Done.** Counters that name a keyed read as keyed (c3e53a2), so the rest is measurable; a
keyed path the narrow read cannot address declines instead of answering "narrowed, absent",
which was a wrong answer about an element that exists. And the keyed merge carries across
what it does not name (f5f04d4).

**Re-root through a non-field segment** (ReadSubtreeRootedAt), wrapping the answer in a
`!key(f)` single-element list -- the RootKeyedListAt construction in reverse, key field from
schemaForScope. Alone this finishes every POSITIONAL element read, with no keying and no
format change, which is why it is worth doing first.

**Project through `!key`** (projectPatchesAt): meeting `!key(f)` with a `(v)` segment next,
select the element whose f is v. The identity the merge already implements, applied to the
patch. Covers the delta range. No format change.

**Index keyed elements in the snapshot** (snap builder; format change, compatible). The
builder learns an array's key field from the schema at snapshot time and records
`items("G")` where it records `items[2]`; the stream state has to carry the key for the
array it is inside. Compatible both ways -- the index is chunk-granular and the reader falls
back to the nearest ancestor, so an old snapshot degrades to today's behaviour. Covers the
base.

Then a keyed read narrows, and rkb7p8v5 can put the head at an element.

## Not on this path

Stating that an element is ABSENT from a keyed list has no vocabulary. That was the third
reason the scope overlay could not be derived (qth3kqe9h12ksxz9j9n0). A delete of a keyed
element meets it, and no amount of addressability answers it.
