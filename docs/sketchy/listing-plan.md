# A listing costs the names: the snapshot as a directory

Issue: `3kgxprskh12krjrmndn0`. The set read (`y2agz9dyh12kse24n9n0`) was built on a claim
that was never true: that `return: path` "reads no node at all". It enumerates a level by
building the container (`setChildren` → `readValueAt`), under the read budget, so a
listing is refused on exactly the containers a listing is for.

This plan makes the claim true, at the storage layer, and fixes a second defect the
investigation measured on the way.

## What was measured

10 000 records of ~150 bytes under one container, written as one commit, read at that
commit (probe, 2026-09-17):

| read | cost |
|---|---|
| stream the container's events, taking names | 107 ms |
| build the container (`Collect`) | 126 ms |
| read each child on its own, 10 000 times | 44 ms **per child**, 7m20s in all |

The last row is the second defect. `openRead` at a child seeks the nearest snapshot at or
above it and then, for every write since, decodes the **whole log entry**
(`dlog.ReadEntryAt`) to project one child out of it. A container that arrived as one write
costs its whole size per child read: O(N²) over a listing with bodies, and the same for
`iterType`, which reads each member's first event. The snapshot that would let a child read
seek (`snap.Index`) is never taken: the path-snapshot policy fires on **records folded**
(`tail > 64`), and this read folds one record.

## Requirements the listing puts on storage

1. **Names at a level, at a commit**, in store order: the direct children of `P`, and each
   one's kind. Time proportional to the page; memory proportional to the page. No node
   built, no whole index read into memory.
2. **Resumable**: a page ends at a name, and the next page starts after it without
   rescanning.
3. **A member's body at a cost bounded by the member**, not by the write that installed it.
4. A scoped session lists what a scoped read of the container holds.

"Proportional to the page" rules out two things that looked cheaper than they are. The
index trie's skeleton holds every path ever written and is resident, which is the floor the
index's residency was built to get away from, not something to lean on: a level of ten
million names is ten million resident nodes, and a listing that walks them all is not out
of memory. And a per-value snapshot index read whole (`snap.OpenIndex`) is the same thing
one file over.

Ordered insertion into the trie was considered and rejected: a persistent ordered insert
per new child is a B-tree, and an append-only diff store is the opposite of one.
Snapshots are where a set is written once, sorted, and never touched again.

## Design: the snapshot as a directory

A snapshot is a value: the subtree at its path as an event stream, with a small chunked
index over it (`snap` package, one entry per ~`GetChunkSize` bytes). It gains a
**directory**: for each container it holds, a table of the container's direct children,

    (segment, offset, size, kind)

sorted as the store sorts them -- document order is name order, since storage sorts
object keys -- where `offset` and `size` locate the child's events; a container child's
own table follows its subtree, at `offset + size`, so no field locates it. `segment` is
the store's spelling of the child: a field, `[i]`, `{n}`, or `"(id=r1)"` for an element
of a keyed array, which is an object of names in the store.

The table is written **after** the container's subtree, so the builder stays single-pass
and append-only: when a container closes the builder knows every child's offset and size
and writes the table there, where `offset + size` of the enclosing container's entry for
it will point. The root's table is last, and the header locates it. The chunked index stays as it is; it is how a
read finds a path, and it is small.

The event stream is unchanged. The trie is unchanged.

### Reading

- **Listing `P` at the snapshot.** Find `P` through the chunked index and its table
  through the parent chain (a table entry's `offset + size` is where its child's table
  is, so descending is one seek per level, which is the shape of a top-down walk). Read the
  table sequentially from the cursor's position: a page reads O(page) entries. A cursor
  resumes by binary search within the table, which is sorted and has fixed-size entries
  apart from the segment, so the search is over an offset array written after the entries.
- **The tail.** Writes since the snapshot that reach `P`, each decoded **once** per
  listing (not once per child), projected at `P` (`projectAt`), and applied to the
  table's names at one level: a plain object adds its keys and removes its `!delete`d
  ones; a total cover (`!insert`, `!replace`, `!delete` at `P`) replaces the names with the
  operand's; an operation that edits by position (`!arraydiff` and kin) falls back to the
  value fold at `P`, streamed, taking names. The tail is bounded by the snapshot policy
  (below), which is what bounds the value fold too. A tail write's names are merged into
  the page in order; a page is emitted only once its names are final.
- **Scope.** The scope's statements after baseline's, in the order `projectScope` gives
  them: a scope claim above `P` is a total cover.
- **Bodies.** `offset` and `size` are the member's events: a member read seeks and reads
  its size. For a member the tail changed, the read is what it is today, and the tail is
  short.
- **Kind.** From the table, so `iterType` costs nothing beyond the listing.

### The snapshot policy sees `largest`

A read already measures `largest`, the biggest entry it projected, and hands it to the
cursor; the policy ignores it. It fires when `largest > K × bytes emitted` (K = 16 to
start), and snapshots **the parent of the path read**. Sibling reads then seek. If the
parent is still small against the entry, the next read fires one level up, and it converges
in depth steps. One condition, using a number already in hand. This is requirement 3 for
snapshotted data by construction, and it stands alone: it fixes the 44 ms per child today,
listing or not.

### What this does not do

- Compaction is untouched: a snapshot's tables live inside it and go where it goes.
- A snapshot written before this has no directory. Listing over one falls back to the
  value fold at `P`, streamed, taking names: O(bytes) time, O(1) memory, as today's
  read without `Collect`. The next snapshot at or above `P` carries tables.
- A historic listing below the newest snapshot uses that older snapshot's tables, or the
  fallback if it has none.

## Storage API

    // Children lists the direct children of kp as of commit `at`, in the view scopeID
    // names, from `after` (exclusive; "" for the first), calling fn until it returns false.
    // A child is its segment, its kind, and where its events are when the snapshot holds it.
    func (s *Storage) Children(at int64, scopeID *string, kp, after string, fn func(Child) bool) error

`setChildren` in the server calls it in place of `readValueAt`; the walk, the paging and the
retspec are unchanged, and the cursor's `after` resumes the table.

## Procedure

Each step ships alone and is tested alone.

1. **Policy sees `largest`.** Test: 10 000 children in one write; the first child read
   schedules a snapshot at the parent; the second child read is under 1 ms.
2. **Measure the directory's size** on a real store before the format changes. Done,
   against verse-docd at commit 4416 (2026-09-17): the document is 4.17 MB as a snapshot's
   event stream, 9 211 containers, 33 294 keys, 125 bytes per key on average, largest
   fan-outs 817, 519, 346. Names alone are 8.3 bytes per key, 6.7% of the stream. With
   each entry's locators the directory is **not** "the key set once more":

   | entry layout | bytes/key | of the stream |
   |---|---|---|
   | name + 3 five-byte varints + kind + 4-byte slot (the plan's first sketch) | 28 | 22.6% |
   | name + offset and size as 2–3-byte varints + kind + slot | ~18 | ~15% |

   Two things the layout should take from this. The child's table offset needs no field:
   the table is written right after the subtree, so it is at `offset + size`. And the
   4-byte slot per entry -- the offset array a resume binary-searches -- is a fifth of
   the entry and is what makes a page O(page) rather than O(fan-out); it stays. The
   fraction falls as values grow (at the 150-byte records of the measurement above it is
   ~13%), and it is proportional to keys, which is the currency the requirement is in.
   The shape repeats for a test as large as a test needs: ×100 is 3.3M keys, 417 MB of
   stream, 60 MB of directory.
3. **Builder writes tables**, and a `snap.Directory` reader with `ReadTable(P)` and
   `Seek(after)`. Tests: every container kind, empty containers, comments, an old
   snapshot opens and answers as before.
4. **`Storage.Children`**: tables plus the names fold over the tail, plus scope. Tested
   against the keys of a value read at the same commit: fresh write, deletes, `!insert`
   over, a positional op (fallback), scope claim, post-compaction, historic commit, each
   container kind, a table-less snapshot.
5. **`setChildren` uses it.** `TestSetMatch_ListsBeyondTheReadBudget` (on this branch,
   failing) goes green; `session.md`'s "reads no node at all" becomes true, and says
   what a listing costs.

## Verification

Storage (≈2.5 min), logd server (≈27 s), libctl and docd (≈40 s), and the probe from the
measurement above re-run at each step: names in O(page), bodies in O(member).
