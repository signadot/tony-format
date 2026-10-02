# proposal: logd refuses a read at a commit it can no longer answer exactly

## Today

With compaction on, a delta replay below the replay floor is refused, and loudly: `Deltas` returns `ErrReplayCompacted`, and a `FromCommit` watch ends with `replay_compacted` (replay_floor.go explains why; a silent subset is indistinguishable from "nothing changed").

A **state read** at such a commit is not refused. `Match` with a `commit` only rejects `commit < 0 || commit > current` (logd/server/session_read.go:45). `openRead` (storage/cursor.go:128) seeks the newest snapshot at or above the path whose commit is at or below the one asked for (`findSubtreeBaseReader`, storage/snap_storage.go:271) and folds the patches after it. Compaction has dropped some of those patches, so the read answers a state the store never held at that commit, and reports success. docd fans the same commit to its sources (docd/server/compose_read.go:85) and passes the answer through.

This is documented as the bargain: "historical reads become approximate" (storage/compaction.go:14), "State at a commit below the floor is still readable" (storage/replay_floor.go:40). The replay floor exists so that a degraded replay is loud. A degraded state read is still quiet.

## Who it hurts

verse's `Store.AsOf` contract says a no-longer-retained revision is an error (verse entity/store.go:97). Its `LogdStore.AsOf` is `MatchAt`, so it gets success. verse reads at a firing's causing commit when the action runs, which for a gated firing is after a person answered, and a completion hook or sequence step reads at the task's recorded occurrence, which survives restarts. Those commits can be days old. Under `verse up` (cutoff 1h) such a rule acts on stale state with no error; staging is about to run with cutoff 1d.

## Proposed rule

A read would refuse exactly when the patches it needs are not all on disk:

- the base the seek found (a root snapshot, a path snapshot, or none, which is commit 0) has commit `S`, and the read is at `C`;
- the read is exact when `S == C` (no patches needed) or `S >= floor` (every patch above the floor is kept, by the floor's definition);
- otherwise it would refuse, with an error that names the floor, the way `replay_compacted` does, so a caller can tell "too old" from "failed".

A read at the current commit never meets this: the root snapshot written at each switch is newer than anything compaction drops. A read at a kept snapshot's own commit, however old, stays answerable.

The refusal would need its own code through docd as well as logd: compose_read.go today maps a failed match to `match_failed`, and a caller has to be able to tell it apart.

## Open

- Whether preconditions evaluated at a commit (storage/precondition.go:221 also seeks a base) should take the same rule.
- Whether `ReplayFloor` should also be on the read response or the pong, so a client can avoid asking. Not needed for the refusal itself.