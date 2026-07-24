# docd: composed-watch may deliver deltas out of order and replay stale deltas that predate its own snapshot

Severity: MEDIUM (possible data corruption if the client applies deltas by receipt order). Confidence: traced, SUSPECTED (depends on client dedup contract).

1) Ordering: begin() (system/docd/server/watch.go:224-233) sets started=true and captures/clears buffered under cw.mu, then flushes buffered events via writeToClient OUTSIDE the lock. A concurrent forward (:190-214) that arrives after the swap sees started==true and writes directly. The buffered (older) event and the new (newer) live event then race on writeMu with no ordering guarantee → a newer delta can be delivered before an older one. A single logd watch guarantees in-order deltas; docd breaks that here.

2) Stale replay: sub-watches are established (NoInit, deltas buffer) BEFORE the snapshot read at current commit (watch.go:142-169). Buffered deltas can carry commit <= snapshot commit; flushing them after the snapshot replays stale writes over newer state.

Both are benign ONLY if the client ignores/dedupes deltas with commit <= last-applied. A normal logd watch never emits a delta older than its init snapshot, so composed watches deviate from that contract. Confirm against the client apply logic; if it applies unconditionally, this is a real correctness bug.

Fix direction: hold ordering across begin()'s flush and live forwards (flush under the same serialization as forward); drop/skip buffered deltas with commit <= snapshot commit. Files: system/docd/server/watch.go:142-233.