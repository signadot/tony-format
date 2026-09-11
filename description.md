# docd: a watch logd ends keeps its mount-coordinator reader token, so an overlapping mount waits forceAfter on a dead watch (forever at "0")

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53 with real
logd + docd.

Seed three commits, compact, watch `a` through docd with FromCommit 1 so logd ends it
with replay_compacted; the client Closes. Then time a mount overlapping `a`:

    control (healthy watch, closed normally)           3ms
    single-route, forceAfter 2s                        2.003s
    single-route, forceAfter "0"                       still blocked at 4s; completes
                                                       1ms after the client disconnects
    composed (controller at a.m, then mount a.n), 2s   2.004s

pumpLogdToClient passes logd's Ended event straight through
(docd/server/client_session.go:331-343), and composedWatch.forward has no Ended case
(watch.go:424-452); docd releases the reader token only on an unwatch, its own end of
the watch, or session close. The client's routeEvent drops the watcher
(libctl/logd.go:861-871) and Watch.Close returns early once it has failed
(watch.go:269-271), so no unwatch is ever sent.

Consequence: after any watch logd ends (slow_consumer, replay_compacted, replay_failed,
invalid_path), every overlapping mount stalls for forceAfter, or until the client
disconnects at "0"; a composed watch also keeps its other sub-watches running.

Also seen: the composed watch first sent a State read below the replay floor, and ended
only after composedReplayWait (10.007s); logd's single-route path refuses before any
state (logd/server/session_watch.go:246-257).

Neighbour: favh47sxh12kraaebsn0 (the same stall on LIVE watches).