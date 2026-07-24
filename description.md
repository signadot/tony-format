# docd/txpool: Get holds p.mu across a deadline-less blocking read → a stalled logd wedges all pooled tx allocation

Severity: HIGH (permanent server-wide hang from an ordinary slow/stalled peer). Confidence: verified.

Pool.Get (system/docd/txpool/pool.go:221) does p.mu.Lock()/defer Unlock() for its whole body and on a cache miss calls fetchTxID (:326) -> readResponse (:337) -> a blocking conn.Read with NO deadline and NO ctx (the ctx passed to Get is only consulted in connectLocked; fetchTxID ignores it). grep confirms no SetDeadline anywhere in pool.go.

Scenario: logd accepts the NewTx but never sends the response (logd hang, or a black-holed TCP path with no RST). fetchTxID blocks forever WHILE HOLDING p.mu. Every subsequent Get (baseline NewTx via serveNewTx, every baseline multi-mount patch via allocTx->Pool.Get), plus refillLoop/topUp/Stats/Close, then blocks on p.mu forever → all pooled tx allocation is wedged server-wide.

Sibling short-lived logd calls (logdread.go readLogdMatch, allocScopedTx) DO set conn.SetDeadline for exactly this — the pool does not.

Fix: set a read/write deadline (or thread ctx) around fetchTxID's I/O; do not hold p.mu across the blocking read.