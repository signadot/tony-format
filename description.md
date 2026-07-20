# docd: mount coordination force-ends legitimate long-lived overlapping watches (fixed 5s force_after; membership_changed loses the gap)

## Summary

A `Mount` that overlaps an existing long-lived watch waits the full `force_after`
(default **5s**, `defaultMountForceAfter` in docd/server/config.go) for that watch to
"drain", then **force-ends** it (`WatchEvent{Ended, EndReason: "membership_changed"}`).
For a persistent watch (a root/subtree watch held for the life of a process), this
means (1) every such mount stalls ~5s, and (2) the force-ended watch does **not**
recover in practice — the reader is permanently broken. Mounting a controller under a
subtree someone is watching — a normal thing — becomes either a 5s stall or a dead
watcher.

## Repro (observed in verse dogfooding, go-tony v0.0.78)

- verse holds a live `Watch` on `verse` (root).
- `verse connect` mounts a read-only content controller at
  `verse.local.dir-content-<scope>` (a descendant of `verse`).
- The mount blocks **5.05s**, then the `verse` watch ends with `membership_changed`.
  Setting `ForceAfter=100ms` makes the mount complete in **105ms** — confirming the wait
  is exactly `force_after`.
- After the force-end, the root watch is re-established (`sess.Watch` returns nil error),
  but **no further events reach the reader**. A `demo:pr` put that produced a gate before
  the connect produces none after — permanently. Re-watching `FromCommit: lastRev` did not
  recover it either.

## The problems

**1. `force_after` is a guillotine, not backoff.** A mount forcibly kills legitimate
long-lived overlapping watches; there is no "just include me in the membership, don't
kill the reader" path. Suggest replacing the fixed wait-then-force with **exponential
backoff** on the membership-update attempt, plus a **graceful membership update** that
does not end the reader's watch. Forcing should be a last resort / opt-in, not the
default at 5s.

**2. Re-establish loses the gap, and the client can't close it.** The terminal
`WatchEvent` carries a `Commit`, but the client-facing `WatchEndedError{Path, Reason}`
**drops it**. A client that wants to re-watch `FromCommit` to replay the gap gaplessly
has no commit to resume from — it can only re-watch from *current*, whose initial
`State` emits nothing, silently losing every delta committed during the
force-end -> re-watch gap. Suggest surfacing the ended-at commit on `WatchEndedError`
(e.g. `Commit int64`) so a client can `Watch(FromCommit: endedCommit)` and resume with
no gap — i.e. **membership change should hand back a commit the watch can reconnect
after**, not just a reason string.

**3. A `Watch`-declining mount poisons overlapping composed watches.** A read-only
controller that returns `libctl.ErrUnsupported` from `Watch` appears to break any
overlapping ancestor watch trying to (re)compose over it. Making `Watch` a blocking
no-op (emit nothing until ctx cancel) is a workaround, but `ErrUnsupported` on `Watch`
shouldn't poison overlapping composed watches — composition should treat a
Watch-declining mount as "contributes no events."

## Suggested direction

- Replace fixed `force_after` with exponential backoff + a graceful membership update
  (don't kill readers by default).
- Put the ended-at `Commit` on `WatchEndedError` so re-establish is gapless via
  `FromCommit`.
- Don't let a `Watch`-declining (`ErrUnsupported`) mount break overlapping composed
  watches.

Related: "docd: compose ancestor reads across mount boundaries (Match/Watch)".