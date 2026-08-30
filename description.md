# logd: a commit that fails after its log append leaves the index behind, and a migration makes it permanent

## What the pending index is

Only a schema migration has one. `StartMigration` builds it (schema.go:143), and its only
caller is a client's `SetSchema` on a baseline session -- `handleSchemaSet`,
session_schema.go:79; a scoped session is refused. It lives until `CompleteMigration` or
`AbortMigration` clears it. Outside that window `GetPendingIndex()` is nil and the dual-write
below does not happen at all.

It is not related to auto-IDs, and there is no other pending index in the tree. During the
window every commit is indexed twice, once under the active schema and once under the pending
one, so that promotion has an index already built under the new keying.

## The window

`commit_ops.go`, in order:

    112   pos, logFile, err := c.s.dLog.AppendEntry(entry)
    139   if err := index.IndexPatch(c.s.index, ...); err != nil { return }        // always
    145   if pendingIdx := c.s.schema.GetPendingIndex(); pendingIdx != nil {
    147       if err := index.IndexPatch(pendingIdx, ...); err != nil { return }   // migration only
          }
    165   c.s.installHead(commit, stepped, scopeID)
    167   c.s.tick.publish(commit, notification)

Two separate faults, and they want separating because only the second needs a migration.

### 139, which is ordinary operation

A return here leaves the entry in the dlog and in NO index, and skips `installHead` and
`tick.publish`: the caller is told the commit failed, no watcher hears of it, and the record is
in the log regardless. Replay reads the log, so the entry comes back on the next start as a
commit the client was told did not happen. This needs no migration and no schema -- it is the
plain write path.

### 147, which needs a migration and is the worse one

A return here leaves the entry in the dlog and in the active index and missing from the pending
one. That would be a transient inconsistency if the pending index were scratch, and it is not:
it becomes the live index.

## Why the migration case does not heal

The original filing offered "accept and document -- migration completion will rebuild pending
index anyway". It does not. `CompleteMigration` (schema.go:173) does

    newIndex := s.schema.PromotePending(commit)
    s.index = newIndex

and `PromotePending` (storage_schema.go:118) hands back `ss.pendingIndex` as it stands. Nothing
is rebuilt, verified or reconciled. The entry is now missing from the LIVE index while present
in the dlog, permanently -- a log and an index that disagree, which is the one thing a rebuild
is supposed to settle.

`reindexForPending` is the only thing that rebuilds, and it is reached from exactly two places:
`StartMigration` (schema.go:149), which backfills before the pending index goes live, and
replay (schema.go:107), which rebuilds it at startup. So the gap self-heals if the process
restarts before the migration completes, and is made permanent if it does not.

## Why "same code path, so it cannot fail" is weaker than it reads

The original filing argued this is likely harmless because `IndexPatch` is the same code for
both indexes, so if active succeeds pending should too. The two calls differ in exactly one
argument, the schema, and the pending schema is the one with the changed `!key` declarations.
Key derivation failing under the new schema and not the old is not an unrelated i/o accident --
it is the shape a migration exists to exercise, and the reason the pending index is built
separately at all.

## Fix

The two faults have one root: work that can fail runs after the record is already in the log.

1. Order the fallible work before the append. Both `IndexPatch` calls are decided by data that
   exists before anything is written; nothing forces either to happen after the log record.
2. Decide what a commit that fails after its append MEANS, and make the log agree with it. As
   it stands the entry is committed to the log and disowned by the caller.
3. For the migration half specifically, verify at promotion rather than trusting it, so
   `CompleteMigration` cannot install an index that skipped an entry. Weakest of the three --
   it detects rather than prevents -- but it is what closes the permanence.

Not reproduced: this is read from the code. A test for the 147 case needs `IndexPatch` to fail
under the pending schema and succeed under the active one, which a fault-injecting index or a
migration to a schema whose keys the data cannot satisfy would give.

## Related

Identified during review of issue #087 (schema migration), a pre-migration numeric id that no
longer resolves. tfn7ptxch12ksv7ca9mg (auto-ID handling during schema migration) is the other
open migration issue.
