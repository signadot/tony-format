# logd: a session's heartbeat ping queues behind its reads, so a burst of watch opens tears the session down

A logd session answers a heartbeat ping on its request loop (`system/logd/server/session.go:353`). That is deliberate: the answer means "this loop is alive" (`session.go:296`). The client allows 5s for the answer, and pings every 10s (`libctl/logd.go:85-86`). Two kinds of request can hold the loop, and a ping sent behind them waits until they are done:

- **A watch open, on every open that is not `waitIfAbsent`.** `handleWatch` calls `absentAt` (`session_watch.go:191`), which is a full `readValueAt` of the watched path. It runs on the loop, before the confirmation, and nothing bounds it. N watch opens put N reads ahead of the ping, one after another.
- **A match, when the session already has 8 in flight.** A match runs off the loop, in one of `maxConcurrentReads` (8) slots. The loop itself takes the slot (`session.go:308`), so a ninth match blocks the loop until one of the eight finishes.

A watch's opening picture (`sendInitialState`, `session_watch.go:397`) is not in those slots. It reads the same value a second time, in the `forwardEvents` goroutine, with no bound on how many run at once. When the watch has no `fromCommit`, it reads at the same commit `absentAt` did.

A client that opens many watches at once falls into this. Examples are verse's versed arming every rule at start, or re-opening every watch a drop ended. The client's own ping queues behind the opens, the heartbeat times out, and the session is torn down. That abandons every watch on it, each watch re-opens, and that is the same burst again. On verse's staging, one timeout became twenty minutes of them (2026-10-30, 20:07–20:31, and 00:19–00:35 the next day). Up to 4,607 watches were abandoned per teardown, and docd's CPU was pinned on initial reads throughout.

verse copes on its side by opening its watches a few at a time (signadot/verse `wz2pp5j0h12krmxewnn0`). That ties the client to how the server queues work, which it cannot see.

A session's liveness check is not a read of the store, and the time to answer it should not grow with the number of reads the client has asked for. A burst of opens should be slow, not fatal. The fix keeps the ping on the loop and keeps the loop from waiting on reads. The plan is in the comment below.

(Corrected 2026-11-01. The first version said a watch's opening picture is read in the 8 slots, and proposed answering the ping off the loop. Neither matches the code.)
