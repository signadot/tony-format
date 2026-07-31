# logd: decide whether a write that changes nothing should become a commit

logd commits whatever it is handed. There is no compare-against-current step
anywhere on the write path: doCommit evaluates CAS preconditions, allocates a
commit, injects auto-ids, merges, writes, indexes, publishes. No DeepEqual, no
Diff. Measured:

    write 1 -> commit 1 (committed=true)
    write 2 -> commit 2 (committed=true)  [byte-identical to write 1]
    write 3 -> commit 3 (committed=true)  [identical again]
    watcher notifications fired: [1 2 3]

A !delete of a key that was never there likewise gets its own commit, with the
state unchanged across it.

## Why it matters

A reconcile-style writer -- a controller re-applying desired state on a timer --
produces a stream of commits that change nothing. Each one currently costs:

  - a log entry and a commit number,
  - a notification to every matching watcher, and
  - for each of those watchers, a full document reconstruction, purely to
    discover that nothing happened. That read is O(patches since the last
    snapshot): measured at 1.6ms at 50 commits and 62ms at 1550, per event, per
    watcher.

It also brings compaction forward, since the cutoff is spent on entries that
carry no change.

The per-watcher read cost is being addressed separately (maintain the current
document once, stepped forward per commit, rather than rebuilding it per watcher
per event). That work makes the change gate cheap but does not remove the
commit, the entry, or the wake.

## The question

Should a write whose merged patch would not change state be suppressed at commit
time?

Suppressing it removes the entry, the notification, the wake and the disk in one
move. Against that:

  - It changes what the log IS. "Someone wrote this at time T" is genuine audit
    information even when the value did not move. A write history and a state
    history are different artifacts, and logd currently is the former.
  - What commit does a suppressed write return? Returning the previous commit is
    defensible ("your write is already reflected at N") but it means a
    successful write does not always yield a new commit, which callers and the
    tx result shape currently assume.
  - Multi-participant transactions: one participant's patch may be a no-op while
    another's is not. The transaction still has to commit atomically, so
    suppression can only ever apply to the merged patch, not per participant.
  - Detecting it is not free either. doCommit reads current state today only
    when a CAS precondition is present; suppression would need that read on
    every write.

## Regardless of the outcome

The watch-side change gate is still needed for the residual: ops that net to
nothing -- a delete of an absent key, a !replace with identical content, an
!arraydiff that cancels. Those are also exactly the cases patchMayAffect refuses
to judge and punts to the authoritative recompute.