# storage: a commit that lands while a schema migration runs is missing from the index the migration installs, and stays missing across restarts

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53.

A 50k-field document (so schema snapshots are slow), one goroutine committing
`w.k<i>` in a loop while StartMigration and then CompleteMigration run: 807 of 3608
ACKNOWLEDGED commits do not read back afterwards. A second run lost 704 of 3037, and
after Close and reopen the same 704 are still missing.

Two windows:

- commits 4-786 landed while StartMigration built its snapshot: createSchemaSnapshot
  takes neither commitMu nor snapMu (storage/schema.go:291-365), SetPending runs only
  after it (:150), reindexForPending stops at the snapshot's commit (:154), and the
  commit path writes the pending index only once one exists (commit_ops.go:142);
- commits 3587-3610 landed during CompleteMigration between PromotePending and the
  index swap (Adopt walks the whole index); s.index is swapped without a lock
  (schema.go:180).

The unlocked tick.publish itself is benign (nil notification, max watermark).

Consequence: committed, already-notified writes vanish from reads and replay, and a
restart does not bring them back; only a full index rebuild from the log does.