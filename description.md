# storage: a second schema migration loses every commit written before the first, and a reopen keeps the loss

Reproduced on main (c4c507e):

    commit {a: 1}; StartMigration + CompleteMigration ({v1: .[string]})
    commit {b: 2}; StartMigration + CompleteMigration ({v2: .[string]})
    read at head  ->  b: 2            (a: 1 is gone)
    commit {c: 3} ->  b: 2  c: 3

A scope's data written before the first migration is gone the same way. After Close
and reopen the loss stays: reads at commits 1 and 2 answer nothing, and the schema is
forgotten too (faweqnhvh12ksynxmdn0).

Cause: the second StartMigration backfills its pending index from activeSchemaCommit
(reindexForPending(activeSchemaCommit, commit), schema.go:159), not from the start of
the log, and skips snapshots. The state as of the first migration lives only in that
migration's schema snapshots, which were added to the index the first CompleteMigration
retired. So the second CompleteMigration installs an index holding neither the entries
before the first migration nor any snapshot of them, and the persisted index is that
one.

The log is intact: every entry is still there, and only the index lost them.
TestMigration_MultipleMigrations checks the schema state and never reads the data.