# logd: after compaction and a restart, a narrow read of a path whose every patch was dropped answers absent

Found by the re-review of the cpqj2tf6 branch. It is on main (046e4212) as well as on the branch; the branch did not cause it.

## Today

A read at a path first asks the index whether the path was ever written (`provenAbsent`, storage/cursor.go:99, through `index.UnwrittenBelow`, index/index.go:243). `UnwrittenBelow` rests on a stated premise: "the trie holds a node for every path any patch has ever touched", so a segment with no node was never written.

Compaction breaks the premise. When it drops every patch that touched a path, the path's value lives only in a root snapshot, and a snapshot is indexed at the root alone. The node for the path survives in memory until the store is closed. After a reopen the index has no node for it, the root still proves itself an object through its other children, and `provenAbsent` answers that the path was never written.

So after compaction **and** a restart:

- `Read(head, nil, "g")` answers absent;
- `Children` of `g` lists nothing;
- `Read(head, nil, "")` holds `g`, with its value.

Before the restart all three are right.

## Reproduction

In package `storage`:

```go
commitValue(t, s, "{g: {k1: 1}}")
commitValue(t, s, "{h: {k0: 2}}")
s.SwitchDLog()
cfg := DefaultCompactionConfig()
cfg.Cutoff = -time.Hour            // every patch is beyond the cutoff
s.Compact(cfg)
head := commitValue(t, s, "{h: {k1: 3}}")
// readSubtreeAt(s, "g", head, nil) holds {k1: 1}
s.Close(); s, _ = Open(dir, nil)
// readSubtreeAt(s, "g", head, nil) holds nothing; readStateAt(s, "", head, nil) holds g
```

Run against main and against ef000e9c: both fail after the reopen, and pass before it.

## Who meets it

Any store that compacts and is then restarted, for each path that has not been written within the cutoff. `verse up` compacts with a one-hour cutoff today. Staging is about to compact with a one-day cutoff (verse ctqj2tf6h12kr8txqxn0): after a logd restart there, a narrow read of anything not written for a day would answer `not_found`, and so would a watch's absence check on it.

Not checked: whether a path snapshot at or above the path changes the answer, and whether the watch and set-listing paths in the server fail the same way (they read through `Read` and `Children`, so they should).

## What would close it

`provenAbsent` would not answer "never written" where a snapshot could hold the path: for a store with a baseline floor above zero, a missing node proves nothing about a path at or below what a root snapshot holds. The read then falls through to the snapshot, as it does before the restart. Whether to narrow that (the index recording which paths a snapshot holds) is a design choice for the fix.