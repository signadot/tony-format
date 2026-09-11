# logd: a watch opened under writes can deliver the delta of a commit its initial state already holds

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53.

A writer committing in a tight loop while 300 watches on `a` open in turn without
fromCommit: 4-8 of the 300 get a delta for the commit their state event already holds,
e.g. state at commit 853, then {commit: 853, patch: {x: 852}}.

Interleaving: commit c is published -- watermark raised (storage/tick.go:74),
notification queued (:77) -- before hub.Watch registers the watcher
(server/session_watch.go:85); the notification is dispatched afterwards (tick.go:129), so
it reaches the new watcher. GetCurrentCommit (:88) already includes c, and the state is
read there (:240). replayedThrough is set only with fromCommit (:243), so live() does not
skip c (:559); it seeds at c-1 (:571) and sends c's delta. libctl does not filter by
commit. (With noInit this is correct, and TestSession_ScopedWatch_QueuedRaceEventNotDropped
depends on it.)

Consequence: the documented order is broken -- a delta the state already holds. Harmless
for stored deltas, which are absolute; a delta the server builds by diffing (deltaAt,
:418-422, DiffWith without DiffAbsolute: scoped re-reads, projections blocked by an
operator above the path) is not idempotent, and re-applying one can fail or corrupt the
client's copy (suspected, not reproduced).