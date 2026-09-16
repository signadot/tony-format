# logd: match with `..` -- an any-depth read over the wire, once a wildcard match exists

Follow-up to y2agz9dyh12kse24n9n0, which gives `match` a wildcard path answered one node at a time. That issue takes wildcards at any level (`a.*.b`, `runs(*).status`) and leaves `..` out, deliberately. This is `..`.

`..` names the nodes at any depth below a point, the node itself included. In process it already works: `ir.ListKPath` walks it (ir/kpath.go:279-291, the descent arm at ir/kpath.go:322-340), `o list ..image deploy.tony` is documented (docs/objpath.md:44-48), and kpaths `Matches`/`MatchesPrefix` now agree with that walk (17hj5ygkh12ks7j7n5n0). What is missing is the wire operation.

## Why it is a separate issue, not an open question on the other one

A single-level wildcard is an enumeration of one nodes children, which the index already does by name (storage/index/index.go:115-119, storage/index/cursor.go:35-60). A descent is a walk of a whole subtree of unknown size, and the index is keyed by literal segments with no notion of depth. The protocol shape is the same; the stores work is not.

It also has to stay refused everywhere else. `..` is a query segment: a path holding one cannot be what a patch is rooted at, what a watch names, or what the index is keyed by (docs/objpath.md:80-88, server/path.go:13-19). Accepting it in a read must not become accepting it in a path.

## What is inherited from the wildcard match

Nothing new on the wire: the answer is the same sequence of one-node results, each carrying its own concrete path and the one commit the set was read at, ended by the marker. A descent is the same answer over a different walk, which is the point of doing it after.

## What has to be decided

- **Cost control.** A subtree of unknown size is a read of unknown size. Is there a bound -- a depth limit, a node budget like the write budget (`storage.WriteBudgetError`, session_write.go:204-207), a cursor the caller pulls -- or is it the callers to stop reading? The one-node-at-a-time shape makes stopping cheap, which is an argument for leaving it to the caller.
- **Order.** Children come out in the stores order at each level; a descent has to fix document order (pre-order, node before its descendants, which is what `visitAll` does, ir/kpath.go:487-504) so a caller can reason about what it has seen.
- **Whether the index can help at all**, or whether a descent is always a subtree scan at the reads commit. This decides whether `..` is a read a server offers or a read a client composes from `.*` levels.
- **`a..b` with more path after the descent** -- `a..b.c` -- is the general case and falls out of the walk, but it is worth saying so rather than discovering it.

docd: the composition question is the same one the wildcard match has, and is tracked there (see y2agz9dyh12kse24n9n0 and its docd follow-up). A descent spanning mounts is strictly harder, since the descent itself decides which mounts it reaches.