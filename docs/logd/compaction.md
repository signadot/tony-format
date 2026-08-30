# Configuring compaction

[The logd overview](index.md#compaction) says what compaction is: a logarithmic
retention schedule that keeps every delta within a recent cutoff and thins older
history down to snapshots. This page is how it is turned on, what each knob does,
and — the part that surprises people — what cannot be asked of it.

## It is off unless the config asks for it

Compaction is the one section that does **not** default on. A config file with no
`compaction:` section, or no config file at all, means compaction never runs and
the full delta history is retained forever.

That is the opposite of `snapshot:`, which defaults to `maxCommits: 1000` and
`maxBytes: 4194304` (4 MiB) even when nothing is configured.

An empty section is not nothing:

```tony
compaction: {}          # compaction ON, every knob at its default
```

Every field defaults when it is absent *or zero*, so `compaction: {}` and a section
naming all five defaults are the same config.

## It runs when a snapshot runs, and never otherwise

Compaction is not scheduled. It is the last step of taking a snapshot:

```
a commit lands
  → thresholds checked (maxCommits, maxBytes)
    → active log switched, so writes keep landing in the new one
      → baseline snapshot written
        → the now-inactive log compacted
```

Four consequences follow from that, and they are the ones worth knowing before
tuning anything:

- **A store that stops taking writes never compacts.** No commits means no threshold
  crossing, means no snapshot, means no compaction — however old its history gets.
  There is no timer and no idle sweep.
- **There is no way to run it by hand.** No `o` subcommand, no request in the session
  protocol. If it has not run, the only lever is writes.
- **It can never run more often than snapshots do.** Lowering `cutoff` below the
  snapshot interval does not make compaction happen sooner; it only changes what
  the next compaction keeps.
- **Turning snapshots off turns compaction off**, whatever `compaction:` says. A
  deliberate `snapshot: {}` disables both thresholds, so nothing switches logs and
  nothing is ever compacted.

Only the **inactive** log is compacted. The log currently being written to is never
touched, so recent history is bounded by the snapshot policy rather than by anything
here.

## The knobs

```tony
compaction:
  cutoff: 1h
  baseInterval: 1h
  slotsPerTier: 8
  multiplier: 2
  gracePeriod: 5s
```

| field | default | what it does |
|---|---|---|
| `cutoff` | 1h | delta records younger than this are kept in full, so reads and replays in the window are exact to the commit |
| `baseInterval` | 1h | the width of the first snapshot tier past the cutoff |
| `slotsPerTier` | 8 | how many snapshots survive in each tier |
| `multiplier` | 2 | each tier is this many times wider than the one before it |
| `gracePeriod` | 5s | how long a reader holding the pre-compaction file has before it is deleted |

### Durations

`cutoff`, `baseInterval` and `gracePeriod` are written the way a duration is written
— `1h`, `90m`, `30s`, `500ms` — which is what `time.ParseDuration` reads. A bare
number is refused rather than guessed at, so there is no unit to remember and no
`3600000000000` to get wrong.

## What a compaction removes

**Not state — memory.** Compaction does not undo the effect of the records it
removes. Immediately before compacting, `SwitchDLog` writes a full baseline snapshot
of the state at the switch commit, and that snapshot is moments old, so it always
falls inside the cutoff window and always survives. Everything the removed records
contributed is in it.

What is removed is the account of *how* the state got there. The store keeps knowing
what it holds; it stops knowing which deltas built it. Concretely:

- reading the **current** state is exact, always, at any retention setting;
- reading a **historical** commit below the cutoff is approximate — it lands on the
  nearest surviving snapshot rather than being replayed to the commit;
- a client resuming a watch from a commit below the retained delta history is
  refused with [`replay_compacted`](session.md) rather than served a gap.

**Delta records** are kept if the record's timestamp is newer than `now - cutoff`. A
record whose timestamp will not parse is kept: the doubtful case keeps history rather
than losing it.

**Scope records are kept regardless of cutoff**, until the scope itself is deleted,
and here the reason is a stronger one — for a scope, the records *are* the state.
Baseline snapshots are written for the root only; scope snapshots are deliberately
not created, because a materialized scope layer resolves `!key` away and is unsound to
re-apply onto a changed baseline, so a scoped read replays the raw op-preserving records
in full. There is no coarser thing to fall back to, and dropping one would
lose state rather than history. Bounding this is tracked in `5hmq80f3h12krh1mbsn0`;
until then a long-lived scope's record set grows without limit and no setting here
changes that. What it costs a *read* is bounded separately: a read that names a path
replays only the records bearing on that path, not the scope's whole history.

**Snapshots** are bucketed by age into tiers past the cutoff. With the defaults,
measuring from the end of the cutoff window:

| tier | covers | keeps |
|---|---|---|
| 0 | the first 1h past cutoff | 8 snapshots |
| 1 | the next 2h | 8 |
| 2 | the next 4h | 8 |
| N | `baseInterval × multiplierᴺ` | `slotsPerTier` |

Within a tier the survivors are the **most recent** ones, not a spread across the
tier, so a tier thins from its old end first.

**The pinned snapshot** — the one carrying the active schema — always survives, in
any tier, at any age.

## What cannot be configured

- **Zero does not mean zero.** Every field treats an absent or zero value as "use the
  default", so there is no way to say *keep no delta history* (`cutoff: 0s` is one
  hour) or *no grace period at all*.
- **The section is not validated when the file loads.** `Config.Validate` checks
  storage durability and nothing else, so `multiplier: 1` — which the policy requires
  to be at least 2 — loads without complaint, then fails inside every compaction
  attempt. Compaction is best-effort at that point: the failure is logged as
  `compaction failed` and the snapshot succeeds anyway, so the symptom is a store
  that quietly never compacts and one error line per snapshot.
- **A misspelled field is ignored.** `cutof:` is not an error; it leaves `cutoff` at
  its default. Same class as [a misspelled request field](writes.md), filed as
  `k0d4y1m6h12kr7cdgdn0`.
- **There is no per-path or per-scope policy.** One retention schedule covers the
  whole store.
- **It cannot be undone.** The delta history a compaction removes is gone; the state
  it described is not, and never was at risk. What cannot be recovered afterwards is
  the ability to read or replay a commit below the cutoff exactly.
- **A reader can be cut off.** After `gracePeriod` the pre-compaction file is deleted;
  a read still holding it errors.

## A worked example

Keep a day of exact history, then thin fast, on a store that commits often enough to
snapshot several times an hour:

```tony
snapshot:
  maxBytes: 4194304     # 4 MiB of delta since the last snapshot; naming the
                        # section leaves maxCommits at 0, which turns the
                        # commit threshold OFF
compaction:
  cutoff: 24h           # a day of exact history
  baseInterval: 24h     # then 24h-wide tiers
  slotsPerTier: 4
  multiplier: 4
  gracePeriod: 30s      # for slow readers
```

Point logd at it with `-config`:

```
o system logd serve -data ./data -config ./logd.tony
```

`o system compose -config` takes the same file.

Writing a section is how a threshold is turned off deliberately: a `snapshot:` that
names only `maxBytes` leaves `maxCommits` at zero, and zero is disabled. Only a
section left out entirely gets the defaults.

Verify what was actually read by the numbers logd prints on the way up:

```
INFO snapshot policy maxCommits=0 maxBytes=4194304
```
