# storage: the index persister keeps the index a migration retires, so the live index is persisted only at Close

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); confirmed at 546bd53 (a probe
prints "persister holds the live index: false" after CompleteMigration).

The IndexPersister is built over s.index (storage/storage.go:181); CompleteMigration
replaces s.index (schema.go:180); the persister keeps persisting p.index
(index_persist.go:65), the retired one.

Consequence (from the code, not run): until Close, periodic persists write the retired
index and never the live one. Under an index ceiling, regions written since the
migration never become durable, so they cannot be evicted and the resident index grows;
a crash reopens on the manifest the retired index last wrote. No wrong read found.

The same swap strands the schema snapshots (see the related issue): everything bound
to s.index has to follow it.