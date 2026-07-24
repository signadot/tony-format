# storage/dlog: compaction is not crash-atomic — in-memory generation resets to 0; no startup recovery of .old/.compact.tmp

Severity: CRITICAL (data corruption / loss on crash). Confidence: traced from source.

Compaction (compaction.go / internal/dlog/compaction.go:249-312 swapLogFile, storage/compaction.go:83-98) rewrites the inactive log to new offsets, bumps an in-memory generation (dlog.go:32-33, atomic.Int64, never persisted), and updates only the in-memory index. index.gob persists only periodically (index_persist.go, every DefaultIndexPersistInterval commits) or on Close.

Failure modes:
1. Crash after the segment swap but before the next index persist: restart loads the OLD index.gob (pre-compaction offsets); index.Build skips entries with Commit <= maxCommit (build.go:25-27) so rewritten entries are not re-indexed; DLog generation resets to 0, matching the stale segments' persisted generation 0 (dlog.go:199-202 guard passes) → reads at old offsets inside the rewritten file → deserialization error or silently wrong entry.
2. If the index WAS persisted post-compaction (generation 1), restart resets DLog gen to 0 → every compacted segment returns ErrCompactionInterrupted forever (nothing bumps gen back).
3. swapLogFile does two sequential renames (path->path.old, tmp->path). A crash between them leaves the live log moved to .old and survivors in .compact.tmp/.old; newDLogFile opens the path with O_CREATE → fresh EMPTY file. There is NO startup scan for .old/.compact.tmp (grep confirms only write-side uses) → the inactive segment is permanently lost.

Fix direction: persist the generation with the index (or derive it from on-disk state); make the index-swap atomic with the log swap (fsync + single atomic rename of a manifest, or write-ahead the swap intent); add startup recovery that reconciles .old/.compact.tmp. This matters even though per-write fsync is intentionally avoided (see the durability issue) — a compaction crash can corrupt data that WAS durable.

Files: system/logd/storage/internal/dlog/compaction.go, dlog.go:199, storage/compaction.go, index/build.go:25, index_persist.go.