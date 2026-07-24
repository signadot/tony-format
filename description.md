# libctl: session mutex held across deadline-less conn.Write/Read → heartbeat cannot fire when the peer stops READING (heartbeat fix incomplete)

Severity: HIGH (permanent session freeze; recovery path self-deadlocks). Confidence: verified.

request() (system/libctl/logd.go:~469) holds s.mu across sendRequestTo -> conn.Write (:667). There is NO SetDeadline/SetWriteDeadline/SetReadDeadline ANYWHERE in libctl (grep empty). Same s.mu-across-blocking-I/O pattern in Watch() (watch.go:137) and connectLocked (logd.go:142-219, dial + hello read at :195 with no read deadline).

Freeze scenario (peer stops READING; TCP alive): the client's TCP send buffer fills, the next conn.Write blocks while holding s.mu. The heartbeat's own recovery is heartbeat -> s.request(Ping) -> s.mu.Lock(); sync.Mutex.Lock is not ctx-aware, so heartbeatTimeout never fires and conn.Close() is never reached → permanent freeze. The heartbeat fix (added to catch 'TCP alive but session loop gone') only covers a peer that keeps READING and drops responses (its heartbeat_test models that); a peer that stops reading is NOT covered, and TCP keepalive doesn't help (peer is alive).

connectLocked variant: after teardown, the next request reconnects; if the server completes the TCP handshake but never sends hello, the hello read blocks forever under s.mu, and Close() (which also takes s.mu) hangs too.

Fix: set write/read deadlines around all wire I/O (and the hello handshake); do not hold s.mu across blocking I/O; ensure the heartbeat recovery path never needs the mutex the blocked I/O holds. Files: system/libctl/logd.go request/connectLocked/sendRequestTo, watch.go Watch.