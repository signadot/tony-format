# logd/libctl: sustained concurrent Match+Patch on one shared session degrades — serial per-conn dispatch + O(history) re-parse per Match (throughput ceiling, not a hang)

Severity: MEDIUM (scalability/latency, NOT a hang/data-loss). Confidence: measured.

TestStress_ConcurrentGatesSharedSessionOverDocd (system/libctl/stress_test.go, 8 gates x 60 iters, each a live Watch + Match+Patch on ONE shared session) PASSES without -race (16.5s) but TIMES OUT under -race (its 8s per-iter wedge-detector trips). It is NOT the old response-loss wedge (that is fixed): in the dump the logd reader is [runnable] in patchNodesFromSegments -> Entry.FromTony -> parse.Parse, the writer is idle, the client readPump is a healthy victim, no goroutine holds a session mutex.

Root causes (compounding on one goroutine):
 - Match reconstructs state by reading AND RE-PARSING O(commit-history) log entries every call (measured: Match 0.3ms @1 commit -> 16ms @200 commits; Patch flat ~0.9ms). No parse cache / snapshot on the hot read path.
 - reader() dispatches serially per connection (session.go:176), so 8 concurrent Matches queue behind one goroutine.
 - watch fan-out also runs on the commit path (see the fan-out issue), adding to per-Patch cost.
Under -race (~10-20x slower) this blows past the 8s test timeout.

Fix direction: cache parsed entries / read from the nearest snapshot instead of replaying+parsing from 0; consider bounded parallel dispatch; move fan-out off the commit goroutine. Files: storage patchNodesFromSegments/ReadStateAt, dlog ReadEntryAt, server/session.go:176.