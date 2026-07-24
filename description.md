# storage/tx: CAS/PatchIf lost update — precondition eval is not serialized with the write

Severity: HIGH (correctness — CAS is not atomic across transactions). Confidence: verified from source.

doCommit (system/logd/storage/tx/coord.go:309) runs GetCurrentCommit -> evaluateMatches(at currentCommit) -> NextCommit -> WriteAndIndex with NO lock spanning eval->write. commitOnce (coord.go:27) serializes only WITHIN one txCoord, and every NewTx builds its own txCoord/commitOnce. There is no global commit mutex (only IndexPersister.mu and storageSchema.mu). NextCommit is atomic (seq/sequence.go NextCommitLocked) so commit NUMBERS are unique, but the read-modify-write is not serialized.

Two concurrent PatchIf with the same precondition on the same key both GetCurrentCommit (=N), both evaluateMatches against state@N (neither sees the other's not-yet-written commit), both pass, both allocate commits and write. One silently overwrites the other, and a CAS that should reject one succeeds for BOTH.

This is the atomicity that a conditional/terminal-once write depends on (e.g. verse's revocable/terminal-once CommitIf gate+goal latch). Current real-world impact is limited only because callers rarely commit the same ref from two goroutines at once — the store-level guarantee is nonetheless broken.

Fix direction: serialize the match-eval..write critical section per key (or globally), or re-check the precondition atomically at commit-number allocation (optimistic: reject if any commit landed on the matched paths between eval and write).

Repro: two goroutines, same path, PatchIf with precondition {absent} (or {v:0}); assert exactly one commits. Files: system/logd/storage/tx/coord.go:309-396, commit_ops.go.