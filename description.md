# logd: a session may hold unbounded watches on one path, and nothing says so

A client which re-watches with a fresh id accumulates watches without bound, and
logd neither refuses nor mentions it. Found as the residue of a verse leak, where
logd was the victim rather than the cause -- but the silence is logd's, and it cost
most of a diagnosis.

WHAT HAPPENED. verse's readiness probe hits /status every 5s, and the handler's
Rev() opened a root watch per call which nothing reclaimed. Measured on a pod 26
minutes old:

    forwardEvents goroutines   158        (131 five minutes earlier)
    states                     158 select, all idle
    parent sessions            2          (92 on one, 66 on the other)
    all on the same path       s.root
    age histogram              6-7 per minute, every minute since pod start,
                               none ever exiting

Nothing was stuck: no chan send (so the outgoing channel drained), no semacquire, no
goroutine in a read. Each new watch is seeded with a full initial-state read at its
path -- 455 KB here -- and dispatch is serial per session, so a registration storm
queues that session's other reads behind it. The probe's own 5s budget then expired
against a 12-18s read, k8s fired another probe, and the cycle fed itself.

WHY LOGD PERMITTED IT. The admission rule (session.go:577-599) is deliberate: a path
is either one id-less watch or N distinct id-bearing watches, so events route
unambiguously. That is right for the case it was written for, and it means a client
using a fresh id per attempt is always admitted. There is no cap: `grep -n
'MaxWatch|len(s.watches)'` over system/logd finds nothing, and no log line reports a
session's watch count.

An idle watcher is never reclaimed either. Broadcast fails a watcher whose buffer
fills, which is the backpressure path, but a watch on a quiet path fills nothing and
lives as long as the session.

WHAT WOULD HAVE HELPED, in order of cost:

  - A warning when a session crosses a threshold of watches, or of watches on ONE
    path. Costs nothing, turns pprof archaeology into a log line, and would have
    named this in the first minute.
  - A per-session cap, refused the way a duplicate watch is refused, saying that the
    earlier ones were never unwatched. A client which leaks then fails fast instead
    of degrading everything else multiplexed on that session.

Neither prevents a client from leaking; both make it a reported fault rather than a
slow decline. A cap is protocol-visible, so where to set it -- and whether to refuse
or only warn -- is a decision rather than a fix.

The leak itself is verse's, and belongs in verse's tracker: Rev() should hold one
watch rather than opening one per call, and /status, the readiness endpoint, should
not read three whole documents for what are counts and refs.