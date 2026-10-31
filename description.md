# logd: a session's heartbeat ping queues behind its reads, so a burst of watch opens tears the session down

A logd session serves at most `maxConcurrentReads` (8) reads at a time, and the session loop blocks on `readSlots` when they are all taken (`system/logd/server/session.go:266,308`). The heartbeat ping is answered from the same loop, after the requests ahead of it, and the client's budget for the answer is 5s on a 10s interval (`libctl/logd.go:85-86`).

Every watch that is not NoInit opens with an initial picture (a State or Absent event, `forwardEvents`), and that picture is a read in those slots. A client that opens many watches at once — verse's versed arming every rule at start, or re-establishing every watch a drop ended — fills the eight slots with opening pictures, the ping waits behind them, the heartbeat times out, and the session is torn down: which abandons every watch, and each re-establishes, which is the same burst again. On verse's staging, one timeout became twenty minutes of them (2026-10-30, 20:07–20:31 and 00:19–00:35 the next day; up to 4,607 abandoned watches per teardown), with docd's CPU pinned on initial reads throughout.

verse copes on its side by opening its watches a few at a time, under the eight (signadot/verse `wz2pp5j0h12krmxewnn0`). That is the client tuned to a server constant it cannot see, and it stops working if the constant moves or an opening picture comes to count as more than one read. The session's liveness check is not a read of the store, and its answer should not depend on how many reads are in flight:

- the ping answered outside the read slots (from the loop before it blocks on a slot, or by a reader of its own), or
- a watch's opening picture not counted among a session's concurrent reads, or bounded separately.

Either makes a storm of opens slow rather than fatal. The first is the smaller change and is what the heartbeat is for.