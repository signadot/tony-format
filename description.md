# logd: a watch on an element does not survive a keying change -- it reports the element deleted, goes silent, and after a re-key cannot be unwatched

Recorded: 090mbrhs item 3 (raise watch deltas under schemaAt of their commit) and item 5 ("a schema commit reaches watchers with its raised rewrite and the new schema"); SchemaSetRequest's doc ("Watchers see the rewrite as a delta at the array"). What a watch on an ELEMENT, or below one, means across a gain, loss or re-key was never decided (the 3n390bjw plan set it aside).

Measured (runs keyed by id, a watch, then the schema change at c3, then a write of n: 10 at c4):

| watch | lose (force) | gain | re-key id -> sku |
|---|---|---|---|
| `runs` | no event at c3; c4 folds to the head read | same | same |
| `runs(r1)` / `runs[0]` (gain), and `.n` under it | c3: `!delete`, absent; c4: nothing | same | same |
| replay `fromCommit: 2`, today's spelling | state absent at c2; c3 `!insert` the element; c4 folds right | same | same |
| replay `fromCommit: 2`, the spelling valid at c2 | invalid_path | invalid_path | not_found (spelled `(sku=r1)`) |

So an element watch says the element was deleted when it was renamed, and misses every write after -- a controller acting on the delete acts on something that did not happen. Replay says it was created at the schema commit. And an id-less watch opened as `runs(r1)` cannot be closed after a re-key: `runs(r1)` is not_watching (`(sku=r1)`), `runs."(id=r1)"` is invalid_path, `runs(A)` is not_watching; the hub keeps it for the session's life. After a loss the literal still closes it.

Also against the record: item 5's "and the new schema" never reached watchers -- a WatchEvent carries no schema, and a schema commit with no rewrite publishes nothing.

Precedent: a mount change ENDS the watches it touches (session_mounted/unmounted), so a watch never observes the change mid-stream and the client re-establishes under the new routing. The same shape fits: end a watch whose path is at or below an array whose keying the commit changes, with its own end reason and the commit as the resume point, and end a replay there too. A read now judges a path by the schema of its commit (3n390bjw); a watch ended at the change is the watch saying the same. Following the element to its new address instead is possible for a re-key and a gain, and meaningless for a loss (a position is not an identity).

Probe: attached (watch_schema_probe_test.go, package server).