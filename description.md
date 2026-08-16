# logd: a stored !pipe executes in the server on every read -- the storage vocabulary is never enforced on a baseline write

`api.ValidateForStorage` declares the vocabulary a stored delta may use and is called on scope
overlays only (`storage/scope_overlay.go:216`). No baseline write is ever held to it. A client
can therefore store an op the charter excludes, and the read path executes it.

## Reproduce

```go
// seed {name: bob}
sess.Patch(ctx, "name", node(`!pipe "tr a-z A-Z"`))   // err == nil, commits
s.ReadStateAt("", commit, nil)                        // -> name: BOB
```

`tr` ran in the logd process, with the daemon's privileges, because a read applies stored
patches. It runs again on the next read, on every replay, and inside every snapshot build --
which is exactly the reason the vocabulary excludes it: "it calls out to the system, so storing
it means re-running it on every replay" (`api/storage_context.go:52`).

## The whole class

Every one of these commits today, and `ValidateForStorage` refuses every one:

| body written at a path | what happens |
|---|---|
| `!pipe "tr a-z A-Z"` | subprocess runs on every read/replay/snapshot |
| `!arraydiff {2: {by: ana}}` | commits; every read fails forever |
| `!strdiff [!insert x]` | commits; every read fails forever |
| `!replace {from: bob, to: rob}` | commits; correct now, wrong once the base moves |

Three of the four are permanent: the entry cannot be un-recorded, and a later patch cannot
repair a read that dies on the way past it.

## Note on scope

How exposed the `!pipe` case is depends on who can open a logd session directly -- verse and
docd sit in front of it in the deployments we have. It is a defect regardless: the store
executes client-supplied commands during a read, which no reader of `ReadStateAt` expects.

## Where the check belongs

One call, after `MergePatches` in `doCommit` (`storage/tx/coord.go:385`), alongside
`InjectKeyTags` which already refuses a patch that conflicts with the schema's keying. It is
O(patch) and touches no document.

One wrinkle to settle first. The vocabulary excludes two different classes for two different
reasons: ops that re-run or call out (`pipe`), which are unsound everywhere; and ops relative
to what was there (`arraydiff`, `strdiff`, `replace`), which the charter itself says baseline
"gets away with ... because its replay is deterministic". Baseline is also where
`MergePatches` MANUFACTURES an `!arraydiff` for every `[i]` write path, so enforcing the whole
vocabulary as-is would refuse array element writes wholesale. Either validate the client's
patch data before merging rather than the merged patch, or state the baseline subset
explicitly.

Seen on main at 30817c5.