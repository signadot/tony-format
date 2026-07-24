# storage: commits are acked before fsync (durability) — DELIBERATE, tracking for crash-recovery/group-commit follow-ups

Severity: NOTE / by-design. The write path (commit_ops.go:33 WriteAndIndex -> dlog AppendEntry buffered file.Write -> in-memory index -> returns Committed:true) does NOT fsync; the only Sync() in storage is the compaction temp file (internal/dlog/compaction.go:179). A power/OS crash after ack can lose recently-acked commits.

DECISION: we deliberately do NOT fsync on every write — this is standard for high-throughput stores (per-write fsync is a large perf hit). Filing only to track the consequences and desirable follow-ups, NOT as a bug to 'fix' by fsyncing every write:
 - Ensure the crash-recovery story is explicit: on restart, the log/index must recover to a consistent prefix (this interacts with the compaction crash-atomicity issue, which IS a real bug).
 - Consider optional group-commit / periodic fsync / a configurable durability level (fsync-on-demand or every-N-ms) for callers that want stronger guarantees.
 - Document the durability contract (acked == in page cache, not on stable storage).

Files: system/logd/storage/commit_ops.go:33-84, internal/dlog/dlog.go AppendEntry, internal/seq/sequence.go WriteStateLocked.