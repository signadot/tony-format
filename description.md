# a client should learn the store's revision without holding a watch open

Verse's Rev() syncs by opening a watch and taking the commit off the initial State
event. That is expensive -- a watch's initial state is a full read at the path, the
last heavy thing on /status -- and it is the wrong shape for the question, which is
just "where is the store now".

Two fixes, both on the wire already or nearly:

1. A match answer has always carried the commit it was read at (api.MatchResult.Commit,
   set to the store head in handleMatch) and libctl.matchAt dropped it on the floor.
   Every Get/List a client makes was carrying the head and discarding it.

2. A ping is the thing a session sends when it is doing NOTHING, which is exactly when
   a revision goes stale. PongResult now carries the head, so an idle session tracks
   the store with no watch, no poll and no read.

Landed:

  - api.PongResult grows Commit. logd answers with GetCurrentCommit (a memory read of
    the tick watermark). docd answers pings itself, so it reports its own high-water
    mark over every commit it has told any client about -- reads, writes, forwarded
    watch events. Documented as monotonic-and-chasing, NOT a commit to read at: docd
    composes mounts with independent commit sequences.
  - libctl.MatchCommit / MatchPatternCommit return the body AND the commit.
  - libctl.KnownCommit(): the highest commit this session has been told about by any
    answer -- reads, writes, watch events, pongs. Fed centrally in the read pump, so a
    caller gets it whether it is busy or idle.

Tests: known_commit_test.go -- a write's commit, a read's commit, another session's
writes arriving on the next read, an idle session learning from the heartbeat, and the
same through docd.

WHAT THIS DOES NOT EXPLAIN. The reported symptom was a revision that only ever landed
on multiples of 1000, read as "the initial State is served from the snapshot, so it
reports the last snapshot commit". logd does not do that: handleWatch takes
currentCommit from GetCurrentCommit (the tick watermark, the head) and forwardEvents
stamps the initial State event with it; with fromCommit set it stamps the CLIENT's
commit, never a snapshot's. docd's composed watch reads at nil commit and stamps the
match's commit, which is also the head. So the snapshot alignment comes from somewhere
else, and switching to KnownCommit may make the symptom go away without the cause being
found. Candidates worth ruling out on the verse side: a fromCommit being fed back into
itself (which would look frozen -- cf. the frozen 254), and docd's virtual clocks
(clock.go), whose value is epoch + n*frequency in NANOSECONDS and so is a multiple of
1000 for any frequency of a microsecond or more.

Related: ap8ddvp2h12krd43gdn0 (read cost), and the watch-accumulation issue.