# a snapshot rode on the write that tripped it, and timed that write out

Staging, verse source reflecting a git ref:

    lastError: "reflecting git:ref:signadot/pull/1677/head:
                replace git:ref:signadot/pull/1677/head:
                write git:ref:signadot/pull/1677/head: context deadline exceeded"

Nothing about that ref is unusual, and it lands on a different ref each time. That is
the tell.

The snapshot policy ran on the commit path. Session.handlePatch calls onCommit ->
Server.maybeCompact -> Storage.SwitchDLog SYNCHRONOUSLY, before that patch's response
is sent. So whichever write happens to be the thousandth (or the one that crosses
maxBytes) pays for a full snapshot of the store -- plus CheckHead, which reads the whole
document -- and, because writes are dispatched on the request loop, every other write on
that session waits behind it too. On a store where a snapshot takes seconds, that is a
client deadline on an arbitrary write, and a retry that arrives just in time to be the
one that trips the NEXT threshold.

It also explains "snapshotting frequently and having a hard time keeping up": the
counter was zeroed after the snapshot, so commits which landed during it were forgotten,
and the intervals drifted.

Fixed: the snapshot runs off the commit path. The switching flag is held for the
goroutine's lifetime rather than the check's, so there is still exactly one at a time;
StopTCP waits for one in flight rather than closing the store under it; and the counter
is decremented by what was counted at trigger time instead of zeroed, so writes during a
snapshot still count toward the next one. Both ends are logged now ("triggering
snapshot", "snapshot complete" with a duration), so the cost is visible without a
stopwatch.

This is safe because of the double-buffered log: SwitchActive flips the active log
BEFORE the snapshot is written, so commits during a snapshot land in the new log. A
snapshot never needed the writer.

Measured by the test, on a 1500-entity store: a snapshot is 320ms, a write is 1ms, and
the write which trips the threshold took 95ms before and under a millisecond after.
TestSnapshotDoesNotRideOnTheWriteThatTripsIt fails on the old code with exactly that
number; TestCommitsDuringASnapshotStillCount pins the counter arithmetic.

How to confirm on staging: correlate the "triggering snapshot" log lines with the
lastError timestamps on the sources. Before this fix they coincide.

Related: 7qayp3hah12kscx2gdn0 (reads off the request loop; writes are still on it, which
is what makes a slow write everyone's problem), ap8ddvp2h12krd43gdn0 (read cost, which
is what makes CheckHead inside the snapshot expensive).