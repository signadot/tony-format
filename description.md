# logd: compaction has a horizon, and its config is validated at load

Two things retention (regx2d1mh12krm0amnn0) needs of compaction.

A horizon: each tier keeps its eight newest snapshots and the tier count is unbounded, so some snapshot from every era survives indefinitely, and a record retention deleted from the state never leaves the disk. compaction.horizon drops any root snapshot older than it, whatever tier it would take a slot in; the newest snapshot in a file is kept whatever its age, and a horizon inside the cutoff is refused.

Validation at load: Config.Validate checked durability and the schema only, so multiplier: 1 loaded and then failed, best-effort and logged, inside every compaction (docs/logd/compaction.md, 'what cannot be configured'). The policy the store will run is now validated where the operator is.