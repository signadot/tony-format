# storage: an in-place write at an array position is lowered, stored and broadcast as the whole array -- only a shifting write needs its siblings

A baseline write at a position is lowered, stored and broadcast as the whole array. Only a
write that SHIFTS positions needs its siblings; an in-place write at d.items[1].v does not.

## Where it comes from

- 9f9b1e8f (Aug 29) added aboveAnyIndex for scope CLAIMS: "a position is not an identity".
  A scope's claim replays over a baseline that moves -- baseline inserts at [0] and a claim
  at votes[1] lands on another element -- so a scope writing by position claims the array,
  shifting or not. That stays right.
- 31d3e27b (Sep 8) made baseline lowering reuse the walk (LowerSites = ClaimPaths plus a
  comment stop), and baseline inherited the collapse without the scope's reason. A baseline
  delta applies to a fixed predecessor, so an in-place positional write needs no siblings.

## Why it is not a one-line change

- Nothing storable says "below a position". MergePatches/RootPatchAt root [i] as
  !arraydiff, and whyNotStorable refuses !arraydiff. So the stored form of
  `d.items[1].v <- 20` today is `d: items: !insert [ ...every element... ]`.
- Reads follow: a read at d.items[2].v counts as wide-operator, through the !insert at the
  array.

## Cost, per positional write, proportional to the array

- the write reads the whole array under the write budget -- a one-leaf write into an array
  over the budget (128 MiB default) is refused;
- the log stores the whole array;
- every watcher at or above the array receives the whole array;
- reads through the array are wide.

## Proposal

1. Vocabulary: store a NON-SHIFTING !arraydiff -- entries only at existing positions, each
   an absolute value or merge, no !insert/!delete entry. ValidateForStorage, whyNotStorable
   and NeedsLowering tell shifting from non-shifting; a non-shifting one with absolute
   entries needs no lowering and is stored as sent. This is a spec change: writes.md and
   one_delta_shape.md list !arraydiff as relative.
2. Sites: LowerSites (baseline) collapses to the array only for a shifting entry;
   ClaimPaths (scopes) keeps collapsing.
3. Index: PatchChildren descends a non-shifting !arraydiff as [i] children (today its
   int-keyed entries would read as {i} sparse paths).
4. Reads: positions move only at array-level records (a shifting write is lowered at the
   array, a cover), so a read at items[i] folds the latest array-level record and then the
   in-place [i] records after it. Probably already so -- array literals are indexed with
   [i] children -- to be verified.
5. Transactions: MergePatches refuses participants mixing a shifting entry with any other
   positional write into one array -- the order-dependent case, which is user error.
   Since c4c507e (j0ynm41xh12ksyxxmdn0) it commits: A `items[0] <- !insert {v: 0}` with
   B `items[1].v <- 20` over [1, 2, 3] gives [0, 20, 2, 3] (insert first); the other order
   gives [0, 1, 20, 3]. Before c4c507e it failed, by accident.
6. Tests: the lowering differential with positional writes, a watcher's fold equal to the
   read, and a leaf write into an over-budget array admitted.

Scopes, keyed arrays and mounts are unaffected.

## Decisions

- Does a non-shifting !arraydiff enter the storage vocabulary? (1) is the crux; the rest
  follows from it.
- (5): refuse non-commuting positional participants, or leave them as user error?