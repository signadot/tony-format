# logd: a scoped read at an old commit can miss a dominated entry compaction dropped, and says nothing

Found while working on cpqj2tf6 (a historical read reports the commit it actually answered), which covers baseline reads only.

## Today

A scoped read whose commit is earlier than one of the scope's live statements takes the historic path in `projectScope` (storage/cursor.go:222). That path folds every scope segment from commit 0 to the commit read. It has no base snapshot.

Scope compaction (storage/scope_compaction.go) drops an entry beyond the cutoff once a later entry of the same scope dominates it. The dominating entry can be far newer than the read.

**Example:** e1 at commit 10 is dropped because e2 at commit 200 dominates it. A scoped read at commit 150 needs e1, since e2 comes later and isn't folded. e1 is gone, so the read answers as though the scope never stated e1, and reports success.

The replay floor doesn't detect this. It is global and records the highest dropped commit (10 here), and the read at 150 is above it. cpqj2tf6's rule, which compares the base snapshot against the floor, doesn't see it either, because the scope term doesn't start from the base.

## Who meets it

Only a scoped read at a commit earlier than some live statement of its scope, after compaction has dropped a dominated entry of that scope. cursor.go:212 already notes that consumers read their scopes at the head.

## What would close it

The scoped read would also report the commit it can answer exactly, as baseline reads do under cpqj2tf6. That needs to know, for the scope, which dominated entries were dropped and which entry dominated each one: an entry dropped at `d` and dominated at `D` makes reads in `[d, D)` inexact. That would be a per-scope record kept by compaction, which nothing keeps today.