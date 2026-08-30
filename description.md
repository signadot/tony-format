# logd: a failed pending-index write is promoted into the live index, so the entry stays unindexed

During a schema migration every commit is dual-indexed, into the active index and into the
pending one. The three steps are not atomic, and the consequence is not the transient
inconsistency this was filed as: the pending index BECOMES the active index, so a gap opened
during the migration is promoted into the live index and stays there.

## The window

`commit_ops.go`, in order:

    112   pos, logFile, err := c.s.dLog.AppendEntry(entry)
    139   if err := index.IndexPatch(c.s.index, ...); err != nil { return }
    145   if pendingIdx := c.s.schema.GetPendingIndex(); pendingIdx != nil {
    147       if err := index.IndexPatch(pendingIdx, ...); err != nil { return }
          }
    165   c.s.installHead(commit, stepped, scopeID)
    167   c.s.tick.publish(commit, notification)

A return at 147 leaves the entry in the dlog and in the active index and missing from the
pending one. A return at 139 is the same shape one step earlier, and skips `installHead` and
`tick.publish` as well: the entry is in the log, in no index, and no watcher hears of it.

## Why it does not heal

The original filing offered "accept and document -- migration completion will rebuild pending
index anyway". It does not. `CompleteMigration` (schema.go:173) does

    newIndex := s.schema.PromotePending(commit)
    s.index = newIndex

and `PromotePending` (storage_schema.go:118) hands back `ss.pendingIndex` as it stands. Nothing
is rebuilt, verified or reconciled. The missing entry is now missing from the LIVE index, while
present in the dlog, permanently -- an index and a log that disagree, which is the one thing a
rebuild is supposed to be able to settle.

`reindexForPending` is the only thing that rebuilds, and it is reached from exactly two places:
`StartMigration` (schema.go:149), which backfills before the pending index goes live, and
replay (schema.go:107), which rebuilds it at startup. So the gap self-heals only if the process
restarts before the migration completes, and is made permanent if it does not.

## Why "same code path, so it cannot fail" is weaker than it reads

The original filing also argued this is likely harmless because `IndexPatch` is the same code
for both indexes, so if active succeeds pending should too. The two calls differ in exactly one
argument, the schema, and the pending schema is the one with the changed `!key` declarations.
Key derivation failing under the new schema and not the old is not an unrelated i/o accident --
it is the shape a migration exists to exercise, and the reason the pending index is built
separately at all.

## Fix

Whatever is chosen has to hold the invariant that the index which becomes live agrees with the
dlog, since promotion is unconditional:

1. Order the fallible work before the append. Both `IndexPatch` calls are decided by data that
   exists before anything is written; nothing forces the pending write to happen after the log
   record.
2. Fail the commit as a whole. A step-3 failure that returns an error while the entry is in the
   log and the active index is already a half-applied commit; the same question applies to
   step 2, which is the pre-existing case.
3. Verify at promotion rather than trusting it, so `CompleteMigration` cannot install an index
   that skipped an entry. This is the weakest of the three -- it detects, it does not prevent --
   but it is the one that closes the permanence.

Not reproduced: this is read from the code. A test would need `IndexPatch` to fail on the
pending schema and succeed on the active one, which is what a fault-injecting index or a
migration to a schema whose keys the data cannot satisfy would give.

## Related

Identified during review of issue #087 (schema migration). tfn7ptxch12ksv7ca9mg is the other
open migration issue.
