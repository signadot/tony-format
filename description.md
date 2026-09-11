# storage: a migration's schema snapshots are indexed into the index it retires, so a store written to afterwards reopens without its schema

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53.

Commit, migrate to a keyed schema, commit once more, Close, reopen:
GetActiveSchema answers nil at 0. Without the extra commit it is present at 3.

Both schema snapshots are added to the index CompleteMigration retires
(storage/schema.go:361, before the swap at :180), and reindexForPending skips
snapshots (:259). The reopen's catch-up starts past the manifest's MaxCommit, which is
past them, so replaySchemaState finds no schema. TestMigration_ReplayActiveState passes
only because it writes nothing after migrating.

Consequence: after a restart the store is schemaless -- keyed arrays lose their
identity rules, and a pending migration is forgotten.