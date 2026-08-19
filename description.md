# docd: a composed watch drops fromCommit, though logd-backed mounts share one commit sequence

startComposedWatch ignores req.Watch.FromCommit and re-inits at current state. The reason
recorded in the code was that "the sub-streams have independent commit sequences, so a
single resume commit cannot replay them". That is false for the ordinary case, and Scott
was right to call it: the transaction mechanism exists precisely so that mounts coordinate
commits.

WHAT IS ACTUALLY TRUE

  - A logd-backed mount writes to logd, so it shares logd's single commit sequence.
  - coordinatePatch allocates ONE logd transaction with a participant per mount, so a
    write spanning mounts is ONE commit -- that is the whole point of it.
  - coordinateMatch already relies on this for reads: a composed read at a commit is
    coherent "because base and logd-backed mounts share logd's one commit sequence".
  - libctl.WatchParams.FromCommit already reaches a controller's Handler, so a
    logd-backed controller can honour a cursor today.

So a resume commit IS meaningful across logd-backed mounts, and dropping it is an
implementation gap.

WHAT IS GENUINELY UNRESOLVABLE, and is the narrower rule

  - A SELF-BACKED controller (one not persisting to logd) has its own timeline and cannot
    answer for a logd commit. A composition including one cannot replay from a commit as a
    whole. docd knows which mounts are logd-backed only by what a controller does with a
    commit it is given, so this may need the mount handshake to SAY it.
  - A mount membership change is a different matter and the current behaviour is right: the
    composition itself changed, so deltas from before it describe a different document.
    Ending the watch and re-initializing is correct there, cursor or no cursor.

WHAT IT WOULD TAKE

  1. Pass FromCommit down to each sub-watch instead of dropping it.
  2. Decide the initial state: the composed state AT that commit (each mount read at it,
     which coordinateMatch can already do) rather than at current.
  3. Merge the replayed deltas across sub-streams in commit order -- they are comparable,
     which is the point above.
  4. Refuse, or fall back to re-init, when a contributing mount cannot answer for a logd
     commit; ideally declared at mount time rather than discovered.
  5. Report the resolved range in the confirmation, as logd does now
     (replayingFrom/replayingTo, v0.0.176).

Relative cursors (-N, "the last N commits", v0.0.176) resolve in logd and so work for a
single-route watch through docd; they hit this same gap on a composed one.

Corrected in the code comments and in docs/logd/session.md as part of filing this.