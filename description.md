# logd: watch fan-out runs synchronously on the commit path → cross-session head-of-line + blocks the session's own heartbeat

Severity: HIGH (a slow watcher degrades unrelated writers; blocks the wedge-detector). Confidence: mechanism confirmed.

WatchHub.Broadcast is invoked synchronously inside WriteAndIndex/patcher.Commit() (system/logd/storage/commit_ops.go:71-82), i.e. on the committing goroutine. Broadcast (system/logd/server/watch.go:146-169) delivers to every matching watcher's Events channel with a per-target time.After(broadcastTimeout)=5s default, SERIALLY (for _, target := range targets).

Consequences:
 - Writer session A's commit ack is delayed up to K*5s by K slow watchers on the path, even though A and the watchers are different sessions. The commit is already durable, so it is latency/availability, not data loss.
 - Because reader() dispatches serially (session.go:176) and Broadcast runs on that same goroutine during Commit(), the committing session cannot answer its own Ping during the stall — the heartbeat meant to detect a wedged session is blocked by the wedge.

Fix direction: hand fan-out to an async per-watcher worker (or a bounded queue) off the commit/reader goroutine; a slow watcher should be failed without blocking the committer or the session's request loop. Files: commit_ops.go:71, server/watch.go:146, server/session.go:176.