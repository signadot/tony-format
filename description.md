# logd: a watch from a commit above the floor can start from a state compaction made inexact

Found while working on cpqj2tf6 (a historical read reports the commit it actually answered).

## Today

A watch with `fromCommit: F` is refused with `replay_compacted` only when `F < floor` (`forwardEvents`, server/session_watch.go). For `F >= floor`, the deltas above F are all kept, so the replay itself is exact. But the watch also starts from the **state at F**: the initial state event (`sendInitialState`), or the replay's base (`seedAt`) when `noInit` is set. That state is read through the same seek a match uses: the newest snapshot at or above the path at or below F, plus the patches after it.

When that snapshot is below the floor, some patches between it and F may be gone. The state at F is then a fold of whatever patches survived, a state no commit held, and the watch labels it F and applies exact deltas on top of it. Nothing reports this.

**Example:** snapshots at 2 and 4, patches through 4 compacted away (floor 4), and a watch from 3. Only commit 3's patch is gone, but the watch's start at 3 is built on snapshot 2. A watch from 4 or later is fine.

## Through docd

A composed watch takes its initial state from a composed read at `from` (docd/server/watch.go). Under cpqj2tf6, that read is answered at the snapshot (`S < from`), so the composed watch would send a state labeled S and then deltas from `from + 1`. That leaves a visible gap. Refusing the logd sub-watch closes it the same way a watch below the floor is closed today.

## Rule

A watch from F is refused with `replay_compacted` when the state at F can't be read exactly (`storage.AnsweredCommit(F, path) != F`), as well as when `F < floor`. Re-watching without `fromCommit` re-initializes, which is what the code already tells the client to do.

A relative cursor (`-N`) is clamped rather than refused: it asks for what there is. Today it's clamped up to the floor, but the floor itself can be inexact. So it would be clamped to the first commit at or after that which reads exactly: the oldest snapshot at or above the path, above the floor, or the head if there is none.