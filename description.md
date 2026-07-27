# dlog: SnapshotWriter mutates logFile.position without logFile.mu, racing Position() (latent — safe only by the A/B active-log invariant)

`SnapshotWriter` mutates `sw.logFile.position` without holding `logFile.mu`, while
`DLogFile.Position()` (`system/logd/storage/internal/dlog/dlog.go:632`) reads it under `RLock`.
That is a data race by the Go memory model.

The unsynchronized writes:

- `SnapshotWriter.Write` — `sw.logFile.position += int64(n)`
- `SnapshotWriter.Seek` — `sw.logFile.position = newPos`
- `SnapshotWriter.Close` — sets `position` to `endPos`, then advances it past the entry

`NewSnapshotWriter` *does* take `logFileObj.mu` for the blob-header write and releases it before
returning (`snapshot_writer.go:73-89`), so the lock discipline is established and then dropped for
the rest of the writer's life. `snapMu` only excludes a second snapshot on the same log file; it
does not exclude `AppendEntry`, which holds `logFile.mu`.


I checked whether this is a live corruption path and I don't believe it is. Appends go to the
active log and snapshots to the inactive one, and `SwitchActive` (`dlog.go:418`) acquires
`inactiveLog.snapMu` at `:431` before flipping, so an in-progress snapshot genuinely blocks the
switch. The A/B invariant holds, so the two writers should not be touching the same `DLogFile`.

The point is that this is only true *by that invariant*, not by the mechanism. Anything that
weakens the invariant later — a third writer, a snapshot on the active log, a change to when the
switch may happen — turns a latent race into corruption with no local signal that anything
changed. And `go test -race` should be able to flag it today with a test that drives a snapshot
and an append at the same time.


Before the WriteAt change that comment read "Atomicity is guaranteed by logFile.mu lock", which was
not accurate for the snapshot path. It has since been rewritten to describe the WriteAt/position
invariant, so this is now just history — but it is the reason the gap survived: the code documented
a guarantee it did not provide.


Take `logFile.mu` around the position mutations in `Write`, `Seek`, and `Close`, or give
`SnapshotWriter` its own cursor and reconcile with `logFile.position` once under the lock in
`Close`. The second is probably cleaner now that `Seek` is virtual — the writer no longer needs
`logFile.position` to be the live cursor, it only needs to publish the final frontier.

Context: found while fixing `pb1aj0sqh12ksp38cxn0` (appends did not land at the position they
reported). The WriteAt change there removed the *other* half of this hazard — `SnapshotWriter.Seek`
used to seek the shared file descriptor, which relocated the append point for any concurrent
writer. Only the unsynchronized `position` mutation remains.