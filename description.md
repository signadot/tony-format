# docd: no write deadline on the client socket → a slow client wedges the mount-coordinator force path and composed-watch forwarding

Severity: HIGH (region-wide watch/mount hang from a slow client). Confidence: verified (no SetWriteDeadline anywhere in writeToClient).

writeToClient (system/docd/server/client_session.go) is a bare conn.Write under writeMu with no write deadline. It is called synchronously from:
 - the mount coordinator force path: beginWrite (mountcoord.go:136-140) force-cancels straggler readers; the cancel hook is terminateWatch (watch.go) -> writeToClient. (Cancels run outside c.mu, but) if the client's socket is full (a slow watcher — the exact thing the force mechanism exists to evict), the Write blocks, so beginWrite never returns its release closure, the writer stays registered in c.writers, and every future beginRead on an overlapping path blocks in cond.Wait forever.
 - composed-watch forwarding.

failAllRoutes runs terminateWatch in a goroutine (go ...) for exactly this reason; the coord force path does not.

Fix: set a write deadline on every conn.Write to a client (a full/dead socket must not block a coordinator/forwarder goroutine); consider async teardown writes as failAllRoutes does. Files: client_session.go writeToClient, watch.go terminateWatch, mountcoord.go beginWrite. Related existing issue: favh47sxh12kraaebsn0 (mount coordination force-ends watches).