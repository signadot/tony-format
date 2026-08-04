# logd: scoped reads cannot be stepped, so every materialization optimisation widens the baseline/scope gap

Split out of x2bn8w56h12krw5we5n0, where the fix for conditional-patch slowness
turned out to help baseline readers only.  Read from source at 4e41c35.

Every optimisation that removes a full state materialization works by keeping a
document and stepping it forward with each commit's patch.  A scoped view cannot
be stepped, so scoped readers keep paying the materialization that baseline
readers no longer do, and the gap grows with each such optimisation.

## Why a scope cannot step

`readScopedStateAt` (`storage/storage.go:225`): the scoped view at commit C is
baseline at C with the scope's OWN patches applied **last**, so they shadow
baseline stickily — a later baseline write to a leaf the scope has written is
hidden, while baseline writes elsewhere show through.

Folding a baseline patch into an already-materialized scoped document loses that
ordering: the baseline write would land on top and overwrite a leaf the scope
owns.  `session.go:754-757` states the same thing from the watch side, which is
why a scoped watcher keeps recompute-and-diff where a baseline watcher steps.

## What currently pays for it

- **Scoped watchers** — recompute-and-diff per event per watcher, where baseline
  watchers step.  The cost the baseline path measured before its fix was 1.6ms at
  50 commits and 62ms at 1550, O(patches since the last snapshot)
  (`session.go:749-752`).  Scoped watchers still pay that.
- **Scoped conditional patches** — `evaluateMatches` calls `ReadStateAt` per
  conditional write (`storage/tx/match.go:22`), inside `commitMu`.  x2bn8w56h
  measures this at 60–500x an unconditional write; its fix is a stepped baseline
  head, which a scope cannot use.
- **No scope snapshots.**  `SwitchDLog` deliberately creates baseline snapshots
  only (`storage/snap_storage.go:117-122`), because a materialized scope overlay
  resolves `!key` away and is unsound to re-apply onto a changed baseline
  (eagjggjdh12ksg00bsn0).  So the scope layer is always replayed as raw patches
  from the snapshot forward, with no way to shorten the replay.

Those compound: a scoped reader replays the baseline patches *and* the scope's
own, from a snapshot that only covers the baseline half.

## Directions

Not evaluated, listed so the shape is on record:

- **Bounded op-preserving scope compaction** — already tracked as
  5hmq80f3h12krh1mbsn0, and the piece that would let a scope have a base to read
  from without resolving `!key` away.
- **Step baseline and scope separately**, keeping two documents and composing at
  read time, so the sticky-shadow ordering is reconstructed per read rather than
  baked into a single stepped document.  Whether the compose is cheaper than the
  replay is the open question.
- **A path-scoped read.**  `ReadStateAt` takes a `kp` it deliberately ignores,
  kept as *"the natural hook for a future path-scoped read"* (`storage.go:184`).
  That would cut both baseline and scoped costs, and is noted there as having
  broken twice when tried (bvm163tyh12krwcqcsn0).

## Priority

Low, on the reported need: the client's scoping needs are secondary.  Filed so
that the growing asymmetry is recorded rather than discovered later — each time
a baseline path gets stepped, this gap widens.