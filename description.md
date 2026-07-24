# logd: single-threaded dispatch + a no-timeout tx blocks the reader forever → TCPListener.Close() hangs on shutdown

Severity: MEDIUM (config-dependent shutdown hang / reader HOL). Confidence: traced.

reader() calls dispatch synchronously (session.go:176); every handler runs on the one reader goroutine. A no-timeout multi-participant tx that never fills blocks in Commit() (tx/coord.go:248-260 else branch waits on ready/expired with no timer). Default Tx.Timeout=1s (fileconfig.go:160) masks it, but Tx.Timeout=0 ('no timeout') blocks the reader forever. Because Run() never returns, handleConnection never calls wg.Done(), so TCPListener.Close()'s l.wg.Wait() (tcp.go:127) hangs forever; session.Close() closing done/conn does not unblock a goroutine parked in Commit().

Fix: bound the tx wait even for Timeout=0 (or reject 0), and/or run handlers so a blocked commit cannot wedge the reader loop and shutdown. Files: server/session.go:176, tx/coord.go:248, server/tcp.go:127.