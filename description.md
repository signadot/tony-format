# logd: a record overwritten often keeps its whole history for the store-wide cutoff, and neither retention nor compaction can say otherwise

A record that is rewritten in place, often, costs the store its whole history, and nothing a client can ask for reduces that. Retention cannot: it deletes items from a container by a date inside each item, and an overwritten record is one item that is always current. Compaction cannot either: its `cutoff` is one duration for the whole store, so keeping a day of exact history for the paths that need it keeps a day of every overwrite of the paths that don't.

Observed on verse's staging store (go-tony v0.0.244), 2026-10-23, over ~3.5 hours of slow-commit logs (every commit there was slow, see m56xswgsh12ks9ravdn0):

| path | commits | what it is |
|---|---|---|
| `verse.verse.iods.github.out` | 8,042 | one 264-byte record: an outbox loop's last pass (`lastPass`, `delivered`, `failed`), overwritten every pass |
| `verse.repo-charters.hotrod.livecheck.<input>~status` | 201 | a delivery status, overwritten as it changes |
| `verse.verse.schedule."repo-charters.hotrod:minute"` | 194 | a schedule's standing tick, overwritten every minute |
| `verse.repo-charters.hotrod.out.git.<input>~status` | 148 | a delivery status |
| `verse.verse.iods.slack.out` | 42 | the slack iod's last pass |

None of them grows. Every one is a single current value whose earlier values nobody reads. Yet with `compaction: {cutoff: 1d}`, every overwrite of every one is kept in full for a day: the active log fills with them, a snapshot is due every ~1,200 commits, and each compaction keeps ~98.5% of the inactive log (`compacting original=67999 surviving=67004`), so the store snapshots and compacts without a break.

The client can only write less often, which verse is doing (slower loops, write on change). That treats the symptom per writer. The store has no way to be told what the writer knows: that a path's history is worthless past its current value, or worth an hour where the rest of the store is worth a day.

## The design question

How should a path, or a part of the tree, say how much of its history to keep, apart from the store-wide cutoff? Candidates, for whoever takes it:

- a schema annotation on a path, read by compaction: history kept for a shorter cutoff, or none past the current value (the newest snapshot keeps the state);
- a per-path cutoff in the compaction config, by path pattern;
- a write option, "this commit supersedes the last at this path for history", which compaction honours.

Whichever it is, it has to keep what reads of the past rely on: an AsOf read below the shorter horizon is approximate, as it is below the cutoff today, and a watch replay over such a path replays from the state rather than from every overwrite.

Context: found while chasing workshop sluggishness on verse staging, with m56xswgsh12ks9ravdn0 (why each of those commits spends 0.5-0.9s in apply). Retention (docs/logd/retention.md) was the first idea for these records, and does not apply to any of them.