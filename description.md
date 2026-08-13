# logd: nothing ever snapshots under `o sys up`, so read cost grows without bound — a 147KB store took 6s to read and took verse down twice

A verse staging deployment lost its engine twice in one day. Both times every write returned
`context deadline exceeded`, the pod never passed readiness, and restarting it changed nothing —
because nothing was wrong with any process. The cost was on the volume.

Four defects, in the order they compound. They are separable and can be split into four issues;
they are together because fixing any one of them alone leaves the outage reachable, and because
the first three explain why the fourth looked like a network fault for a day.

## The measurements

One staging store, go-tony v0.0.126, `o sys up -data /data` in a StatefulSet, three source
clients and one engine.

    /data/logA   15 MB          the delta log
    /data/logB   0 bytes        no snapshot has ever been taken
    the document 147,946 bytes  ~280 entities across 18 slices

Against that store, through docd's client face:

    match verse                 6.4 s
    match verse.git.ref         12.5 s   (5,933 bytes, 45 entities)
    match verse.github.repo     12.7 s   (388 bytes, 1 entity)
    patch (one small entity)    timed out

Reads cost seconds for a document that fits in a network packet, and the cost tracks the length
of the delta log rather than the size of the data. Deleting `/data` and restarting, same code,
same clients:

    match verse                 201 ms   (empty)
    patch                       204 ms
    match verse (139 commits)   659 ms   (78 KB reflected back in)

## 1. Nothing ever snapshots under `o sys up`, so read cost grows without bound

`server.go:103` returns before the snapshot policy is consulted:

    if cfg == nil || cfg.Snapshot == nil {
        return
    }

`MaxCommits` and `MaxBytes` are then both zero, and `maybeSwitch` requires `> 0` on either to
switch. A server given no config never snapshots — not rarely, never.

The only way to supply that config is `o system logd serve -config <file>`. **`o system up` has no
`-config` option at all** — its flags are `-data`, `-logd-addr`, `-docd-addr`, `-docd-mount-addr`,
`-admin-addr`. So a system stood up the documented way cannot be configured to snapshot, and its
delta log grows forever by construction. `logB` at 0 bytes after weeks of writes is that sentence
as a fact.

This is the defect that took the deployment down. It has no threshold anyone crosses and no
symptom until the numbers above; it degrades monotonically from the first commit.

**What needs to change:** a default policy that bounds the delta log, so a store that is never
configured still survives — a system whose default is a guess is better than one whose default is
unbounded growth. And `o sys up` should take `-config` (or the snapshot knobs directly), so the
guess can be overridden where it matters. Either alone leaves half the hole open: a default with
no override cannot be tuned, and an override with no default only helps operators who already knew.

## 2. `hasRootBelow` re-parses kpath strings inside a loop, and it is 63% of the server's CPU

A 20-second CPU profile of the wedged process — 38.26s of samples, ~2 cores pegged, taken while
one client did ordinary reads:

    flat  flat%   cum   cum%
    1.30s 3.40%  19.44s 50.81%  ir/kpath.parseKFrag
    0.16s 0.42%  23.52s 61.47%  logd/storage/internal/patches.hasRootBelow
    0.11s 0.29%  24.23s 63.33%  logd/storage/internal/patches.buildPatchValueIndex
    2.04s 5.33%  20.81s 54.39%  runtime.mallocgc

The chain is `StreamingProcessor.ApplyPatches` → `buildPatchValueIndex` → `hasRootBelow` →
`kpath.Parse` → `parseKFrag`, with `mallocgc` at 54% underneath it: the index build parses the
same path strings again for every candidate it examines, allocating a fresh `KPath` each time.
Half the machine is spent turning strings that were already strings-with-known-structure back
into paths.

This is what makes defect 1 fatal rather than merely untidy. A delta log that must be replayed is
one thing; a replay whose per-entry cost is a superlinear parse is what turns 15 MB into
twelve seconds.

**What needs to change:** parse each path once. The index has the paths in hand when it is built,
so it can carry the parsed `KPath` beside the string, or be keyed by the parsed form outright.
This is a local fix with a large constant behind it.

## 3. An abandoned `Watch` leaks a server-side watcher

The same process held **115 goroutines, 92 of them parked in
`logd/server.(*Session).forwardEvents`** waiting on `watcher.Events` — watches nobody would ever
read again, on a docd that had been restarted minutes earlier.

`libctl/watch.go`, when the caller's context expires while the watch request is in flight:

    case <-ctx.Done():
        s.mu.Lock()
        delete(s.pending, id)
        delete(s.watchers, id)
        s.mu.Unlock()

The client forgets the watch; the server is never told. The session is still alive on the same
connection, so the server has no way to infer it — from where it stands, this is a healthy client
with a watch it does not read. Every commit thereafter fans out to that watcher too, and the
population only grows, because the condition that makes a client abandon a watch (a slow server)
is the condition that makes it retry.

**What needs to change:** abandonment should send an unwatch, best-effort, on the way out. It is
the client that knows.

## 4. `connectLocked` holds the session mutex across an unbounded retry and a fixed 30s handshake, ignoring the caller's deadline

Both `request()` and `Watch()` do `s.mu.Lock()` and then `ensureConnected(ctx)`, so a reconnect
happens with the session's one mutex held. Inside, `connectLocked` loops — dial, back off, dial —
and consults `ctx` only *between* attempts. Hand it a context with no deadline and it holds the
mutex until the process exits.

Bounding the context is not sufficient, which is the part worth reading twice. The handshake read
takes its own deadline, unconditionally:

    _ = conn.SetReadDeadline(time.Now().Add(s.wireTimeout))   // 30s default
    ... sendHello / readResponseWith ...

So a caller who asks for 10 seconds still holds the mutex for 30 when the peer accepts the TCP
connection and then says nothing — which is precisely what a blackholed route, a rolled pod, or a
server too busy to answer looks like.

What this does to every other caller is worse than a slow request, because **a Go mutex takes no
context**. A writer with a ten-second budget does not spend it waiting for the store; it spends it
queued on a lock, is handed the lock after the outage, and only then discovers its deadline went
by. Every caller then reports `context deadline exceeded` on a request that never reached the
wire — an error that names a timeout the caller did not actually wait for, on an operation that
never happened.

I have this as a reproduction in verse (`entity/reconnect_test.go`,
`TestWatchReestablishDoesNotStarveWrites`): a store with one subtree watch open, a route that goes
dark and is healed 20 seconds later. Without a watch it recovers in 4 ms (clean close), 15 s (dark
route, bounded by the session heartbeat) or 4 ms (peer replaced over the same volume) — all three
pass, and the session's reconnect is good. With one watch open it does not recover at all: one
write attempt in a 20-second window, after connectivity was fine again.

**What needs to change:** the handshake read should honour the caller's deadline —
`min(wireTimeout, time until ctx deadline)` — so a bounded caller is actually bounded. Better
still, a reconnect should not be serialized behind the mutex that every request needs: a separate
connect mutex would let waiters fail fast on their own budgets instead of inheriting someone
else's patience.

## Why they belong together

Defect 1 makes reads slow. Defect 2 sets how slow. Defect 3 adds fan-out work per commit as
clients give up and retry. Defect 4 converts "slow" into "stopped", and reports it as a timeout
that points at the network. A reader who fixes only the parse loop buys time; only the snapshot
policy makes the growth bounded; and until 4 is fixed, any future slowness in this stack will
present as an unreachable store rather than a slow one.