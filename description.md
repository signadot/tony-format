# dlog: a compaction swap is not fenced from readers, so a read in flight gets another record or a closed file

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53.

- Section readers: a reader from OpenReaderAt on the inactive log, then CompactInactive
  with a 300ms grace period. Compaction waited 333ms ("timed out waiting for readers
  remaining=1"), and the reader's next read failed with "file already closed". The
  swap closes the handle up front (internal/dlog/compaction.go:257); GracePeriod only
  delays deleting .old, though its doc promises readers that long.
- ReadEntryAt: 4 goroutines reading old positions under the old generation during
  CompactInactive, 200 rounds: 385 reads SILENTLY returned a different entry ("asked
  for commit 116's position, got commit 133"), 415 failed with "reached EOF while
  reading length prefix".

The generation is checked at dlog.go:426, before the file lock at :789; the swap holds
that lock across close, rename, fsync and open, and bumps the generation only after
unlocking (compaction.go:256-315). Nothing in storage retries ErrCompactionInterrupted,
so even a correctly refused read fails.

Consequence: during every compaction after a log switch, a read touching the inactive
log -- the root snapshot lives there -- can fold the wrong record, silently, or fail.

Neighbours, not duplicates: 7d7hhbhmh12ksebkcxn0 (SnapshotWriter position race),
656g8yt5h12krdrmcdn0 (compaction crash-atomicity, closed).