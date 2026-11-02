# logd: after a compaction, a read at a commit well inside the cutoff is answered at the switch snapshot when no root snapshot stands between the baseline floor and it

## Seen

On a local verse (`verse up`: compaction `cutoff: 1h`, the default snapshot policy of 1000 commits or 4 MiB), a rule fired at commit 1898 and spawned a task that ran 9 seconds. Its completion hook read the rule's subject at the firing's commit, as verse's hooks do, and the store answered:

```
reading …project.tree at commit 1898: … compaction answers it at rev 1929: the cursor is older than what the store retains
```

1929 is the root snapshot the compaction wrote at its switch, between the firing and the hook. The patches for 1898 were seconds old, well inside the hour, and still on disk. (signadot/verse-workshop#7, "Found along the way", the steward charter's `onDone`; the charter now avoids reading its subject in a hook.)

## Why

`Storage.AnsweredCommit` (`system/logd/storage/replay_floor.go:97`) answers a read at `at` exactly when the newest root snapshot at or below `at`, call it S, is `at` itself or is at or above the baseline floor. Otherwise it answers at the ceiling, the oldest root snapshot at or after `at`.

`Compact` raises the baseline floor to `droppedBaselineFloor(records, survivors)` (`compaction.go:89`): the highest commit of a baseline patch it dropped, which is a patch older than the cutoff. The switch writes a root snapshot before anything is dropped, so there is always a ceiling.

Put together: for any commit `at` after the floor, the fold is exact only if a root snapshot was taken between the floor and `at`. Root snapshots come every 1000 commits (or 4 MiB). On a store that commits fewer than 1000 times per cutoff period, no such snapshot exists, S is below the floor for every commit up to the switch, and every one of them is answered at the switch snapshot. The window of inexact reads reaches the head, and it opens at every compaction.

On verse staging (cutoff 1d, about 1000 commits an hour) the same compaction on 2026-11-02 raised the floor to 845838 at head 916445, and a root snapshot stands every ~1000 commits above it, so the window there is a day old and nobody notices. The quiet store is the one that gets it: a local verse, or any deployment where the cutoff period holds fewer commits than the snapshot policy's count.

## What the caller does with it

verse reads at the revision asked or not at all (`entity/logdstore.go`, `matchAt`): an answer at another commit is `ErrReplayTooOld`, because a hook or a gate acting on a world it was not evaluated against, silently, is worse than a refusal. So the refusal is right; what is off is that a commit seconds old is not exactly readable.

## Proposed

Compaction drops no baseline patch newer than the newest root snapshot that is itself older than the cutoff. Then the baseline floor is always at or below a root snapshot commit, S is at or above the floor for every commit after it, and every read inside the cutoff folds exactly, which is what the cutoff promises ("within the cutoff replay is exact to the commit", `replay_floor.go`). The patches kept beyond the cutoff are at most one snapshot interval's worth.

The alternative, a root snapshot at the cutoff point taken by the compaction before it drops, keeps the same invariant at the cost of a second snapshot per compaction.

Context: found while reading signadot/verse-workshop#7 against verse main; the hook is verse's `onDone` reading `$subject.value` at the firing's revision (`trigger/trigger.go`, the read `AsOf(oc.commit.Rev)`).