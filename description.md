# logd: schema migration -- a schema change is a commit; delete the pending index and the two-phase protocol

Schema migration breaks data it is not about. This is the design to replace it, and the
plan. No compatibility constraints apply.

## What is wrong

Open: gdpv3fsv (commits during Start/Complete missing from the installed index),
faweqnhv (schema snapshots in the retired index; schemaless after reopen), rgpn3v9v
(the persister keeps the retired index), 4jw0pz1r (a second migration loses everything
before the first), k237zrhw (a refused schema is storage_error). Closed, same design:
q6f0ybmk, tkn7ptxc, tfn7ptxc.

Causes:

1. The pending index is a copy with no reason to exist. Stored deltas carry keyed arrays
   in the stored form, so IndexPatch takes no schema: the pending index holds what the
   live one holds. It is built by a second writer (backfill + dual-write) and swapped in
   at Complete, so every way the copy differs becomes permanent: the Start and Complete
   windows, everything before the previous migration, every snapshot segment, and
   whatever else is bound to s.index (the persister).
2. Schema changes are not commits. createSchemaSnapshot allocates a commit outside
   commitMu and writes the inactive log without snapMu: a snapshot at c can miss an
   in-flight c-1, and it is unordered with compaction and path snapshots (the latter
   from reading, not run). Start's identity check can be invalidated by writes before
   Complete.
3. The schema lives only in snapshot entries, found by scanning the index's snapshot
   segments, so it is lost wherever they are.
4. Two sources of truth: the persisted schema, else the config file's. A config change
   between restarts re-keys with none of StartMigration's checks.
5. The identity check reads baseline only; a scope's positional elements at a path that
   gains an identity are not considered (from reading).
6. The two phases buy nothing: a usePending session reads and writes under the active
   schema -- only hello differs -- and MigrationPatch is gone.

## Design

A schema change is a write: one log, one sequence, one index, and the schema in force
is a function of the commit.

1. A schema change is an ordinary log entry with a Schema field, committed by doCommit
   under commitMu: numbered, indexed, published, notified. It may carry a data patch in
   the same commit, lowered under the new schema. No snapshot, no second index.
2. The store keeps the schema history [(commit, schema)] and schemaAt(c): persisted with
   the index manifest, rebuilt by the catch-up walk. Every snapshot records the schema in
   force at its commit, so compaction may drop old schema entries.
3. Every consumer takes a commit: lowering and preconditions (already under the lock);
   canonicalization moves into doCommit (or is refused retryably when the schema moved);
   raise for reads and watch deltas uses schemaAt of that commit.
4. Identity changes are rewrites in the schema commit, not refusals: gain derives names
   from the key fields (auto-id generates missing ids -- replaces tfn7ptxc's two phases);
   lose turns names back into an array in name order; change renames. Each array is a
   total cover at its path, read under the write budget; a missing key or duplicate name
   refuses, naming the path and element. Scopes: refuse (v1) where a scope has live
   statements under the path, naming the scopes.
5. API: `schema set {schema, patch?, dryRun?}` -> commit, or the dry-run report;
   `schema get {at?}`; a schema commit reaches watchers with its raised rewrite and the
   new schema; a refusal gets its own code. Deleted: Start/Complete/Abort, pending index,
   dual-write, reindexForPending, the swap, usePending, schema snapshots and statuses,
   replaySchemaState, compaction's pending/aborted cases, and migration_in_progress,
   no_migration_in_progress, no_pending_migration, migration_aborted.
6. The config schema bootstraps an empty store only. A store whose schema differs from
   the config's refuses to start, naming both and how to apply it.
7. No dump and recreate. An offline rebuild, or a per-request write budget on schema set,
   is the escape hatch for a rewrite past the budget.

## Plan

- Phase 0 (stop the bleeding): delete the pending index and the swap; hold commitMu and
  snapMu across a schema snapshot; re-check identity at Complete. Fixes gdpv3fsv,
  faweqnhv, rgpn3v9v, 4jw0pz1r.
- Phase 1: the schema change as a commit, schema history, schemaAt, the API collapse,
  the config as bootstrap; delete the migration machinery.
- Phase 2: identity rewrites (gain, lose, change, auto-id backfill), dry run, scope
  refusal.
- Phase 3 (optional): scope claim rewrites; the budget escape hatch.

## Decisions

1. One atomic schema commit, a dry run in place of pending? (recommended)
2. Config schema: bootstrap + refuse on mismatch (recommended), or converge at startup?
3. Losing an identity: allowed as a rewrite into name order, or still refused?
4. Identity change under live scope statements: refuse (v1) or rewrite?