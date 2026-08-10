# Bounded scope overlay — refreshed dev plan

Refreshes the approach in issue `5hmq80f3h12krh1mbsn0`, which its own status report
(discussion/004) asked for before the overlay is built.

Three things changed from that approach. Two of its premises do not hold. And the seam is
not where it looked: the overlay wants to be recomputed at **snapshot** time, not at
compaction time, which is what bounds the cost and what keeps the first phase from being
broken.

Everything marked **measured** was taken against `ad36016` by the test named. Everything
marked **proposed** is a decision this plan is asking for.

---

## 1. Why

**Measured** (`scope_scaling_test.go`), taken immediately after a snapshot — which is
baseline's BEST case, not its steady state. Document size held constant; only the number
of accumulated writes varies.

| N writes | baseline read | scoped read | baseline CAS write | scoped CAS write | baseline watch/event | scoped watch/event |
|---:|---:|---:|---:|---:|---:|---:|
| 50 | 14.5 µs | 774 µs | 371 µs | 1.81 ms | 881 ns | 877 µs |
| 100 | 17.8 µs | 1.72 ms | 367 µs | 2.67 ms | 945 ns | 1.69 ms |
| 200 | 12.1 µs | 3.74 ms | 547 µs | 5.14 ms | 1.08 µs | 3.71 ms |
| 400 | 13.5 µs | 7.83 ms | 389 µs | 10.55 ms | 1.03 µs | 7.85 ms |

**Baseline is fast for two different reasons, with two different bounds**
(`baseline_since_snapshot_test.go`). Holding the snapshot fixed and adding M commits after
it separates them:

| M commits since snapshot | baseline read | baseline CAS write |
|---:|---:|---:|
| 0 | 15.8 µs | 372 µs |
| 50 | 750 µs | 391 µs |
| 100 | 1.54 ms | 436 µs |
| 200 | 3.63 ms | 529 µs |
| 400 | 7.40 ms | 742 µs |

The **read** grows ~470× — it is O(patches since the snapshot), with nothing cached, and
only a snapshot resets it. The **conditional write** grows 2×, and that residue tracks
index growth rather than replay: it is O(patch) because `headStateAt` serves it from a
document `stepHead` folds each commit into (`head.go`). A baseline **watch event** is the
same trick in `session.go`, at ~1 µs.

So the honest comparison is not "a scoped read is 580× a baseline read" — that was against
baseline's best case. At 400 patches of replay each, they cost about the same (7.40 ms vs
7.83 ms). **The per-patch cost is identical; what differs is that baseline's counter
resets at every snapshot and the scope's never does, and that baseline's two hot paths do
not replay at all.** Which is exactly the shape of the fix: give the scope a snapshot
(§3.1) for reads, and give it the stepping baseline already has (steps 7–8) for the other
two.

In order of how much they hurt:

1. **Scoped watch is O(events × scope-writes).** A scoped watcher cannot step its
   document, so `emitScopedDelta` recomputes the whole scoped view per event
   (`session.go`). At N=400 that is ~7600× a baseline watcher, times watcher count.
2. **A scoped CAS write is O(N), inside `commitMu`.** `MatchStateAt` serves baseline from
   the stepped head but falls through to a full scoped read for a scope
   (`commit_ops.go`). The lock is store-wide, so one scoped conditional write stalls
   *every* committer for ~10 ms at N=400. Scope cost is not contained to scope users.
3. **Scoped reads are O(N)** and never truncate: scope patches are exempt from both
   snapshotting and compaction (`snap_storage.go`, `compaction.go selectSurvivors`).

Beyond latency: compaction rewrites the entire retained scope history of every scope on
every `SwitchDLog`, and a scope's patches pin their log segments until `DeleteScope`.

Cheap and independent of everything below: `readScopedStateAt` calls
`LookupRange("", nil, &commit, scopeID)`, which returns baseline **and** scope segments
over all history and discards the baseline ones. A scope with one write pays a scan and
sort of every retained baseline root segment (29 → 64 µs as baseline grows 50 → 400).

---

## 2. What must not change

| Behaviour | Pinned by |
|---|---|
| A scope's write shadows a *later* baseline write to the same leaf | `TestScope_COW_ScopeWinsOverLaterBaseline`, `TestScope_COW_AncestorClobber` |
| Baseline changes elsewhere still flow through | `TestScope_COW_TracksOngoingBaseline`, `TestScope_COW_AncestorMerge` |
| A scope's keyed-list addition survives ongoing baseline keyed additions, and does not leak to baseline | `TestScope_COW_KeyDurability` |
| A scope's `!delete` is sticky against a later baseline write of the same key | **nothing** — `scope_overlay_premise_test.go` (new) |
| A later scope write that *replaces* a container erases the leaves it replaced | **nothing** — `scope_overlay_premise_test.go` (new) |
| Scope patches survive compaction | `TestScope_COW_PatchesSurviveCompaction` |

---

## 3. The construction

### 3.1 Anchor at the snapshot, and recompute every snapshot

```
overlay(T) = Diff( baseline_state@T , scoped_state@T )       for each live scope
```

computed inside `createSnapshot`, at the same commit `T` as the baseline snapshot it is
written beside. A scoped read at `C` is then:

```
baseline snapshot@T  +  baseline patches (T, C]  +  overlay(T)  +  scope patches (T, C]
```

— structurally identical to how a baseline read already works, with the scope layer
getting the same treatment its baseline does.

**This is the choice that makes the whole thing tractable**, for three reasons:

- **Cost is bounded by snapshot cadence, not by scope lifetime.** A scope alive for a week
  replays only its writes since the last snapshot. Tying the overlay to the *compaction*
  cutoff instead would leave a long-lived scope replaying days of writes — the exact case
  that hurts most.
- **No historical baseline state is ever needed.** An overlay anchored at each write's own
  commit would need baseline as it was then, and compaction approximates old commits away.
  Anchoring at `T` and recomputing means the only baseline state ever read is the current
  one.
- **The divergence window is one snapshot interval.** Any concern about an overlay meeting
  a baseline that has moved under it is bounded by how long since the last snapshot, not
  by the compaction cutoff.

Recomputation does not accumulate: `scoped_state@T2` is itself computed from
`overlay(T1)` plus the patches since, so each cycle re-anchors to current baseline and
carries the previous decisions forward unchanged.

### 3.2 What freezes, and when

**A scope's operation re-evaluates against baseline until the next snapshot, then
freezes.** After that it is the value it produced, not the operation that produced it.

That is the same shape as baseline history degrading at snapshot boundaries, and it is the
semantics the COW design already states: *"once scope s1 has written a.x, it owns that
path in its view."* Ownership is absolute; op re-evaluation is relative. The two have been
in tension since the COW fix landed.

**Measured** (`scope_relativeop_test.go`), and why the decision cannot be dodged: logd
applies patches through the full `tony.Patch` dispatcher with no restriction, so every
registered mergeop is reachable from a client patch — `!rename`, `!strdiff`, `!arraydiff`,
`!jsonpatch`, `!if`, the tag ops — and they re-evaluate:

```
baseline {a: {x: 1}}        → baseline: {a: {x: 1}}
scope    {a: !rename x→y}   → scoped:   {a: {x: 1  y: 1}}
baseline {a: {x: 2}}        → scoped:   {a: {x: 2  y: 2}}
```

`y` tracks `x` from 1 to 2. The scope layer is a *function of the baseline*, not a value,
so no materialization can stand in for it without choosing to freeze it. The original
approach assumed this away: *"a relative leaf op would need more, but those are not
believed to exist here."*

The freeze is the price of a bounded base. There is no version that bounds replay and
keeps ops live, because preserving the ops *is* the unbounded log.

### 3.3 This is not the old scope snapshot

Worth stating plainly, since it looks like the thing that was removed as unsound. A
materialized scope snapshot resolved `!key` away and re-applied positionally onto a
changed baseline, truncating. The overlay is a **patch**: absolute in content,
op-*carrying* in form, with `!key(f)` restored by the annotation pre-pass in §3.5. It is
the sound replacement `5hmq80f3h12krh1mbsn0` was opened for.

### 3.4 Three requirements, all measured

`scope_overlay_diff_test.go` builds the overlay exactly as §3.1 says, advances baseline
past `T`, and compares against the replay layer. Two invariants hold as-is; two fail, each
for a specific reason.

**Holds.** Baseline changes elsewhere flow through (the diff is minimal, so the scope does
not own `a.y`). A container replaced by a scalar stays replaced — and note this is the
case that made an earlier draft carry commit-stamped entries and a merge/replace
classifier. Diffing two materialized states leaves no stale descendants to prune, so that
machinery is gone.

**R1 — every form in the overlay must be unconditional.** `Diff` emits
`!replace{from,to}`, a *checked* replace that verifies the document still equals `from`.
Applied over a baseline that has moved it does not mis-apply, it **errors**:

```
overlay: {a: {x: !replace {from: 1 to: 5}}}
apply:   replace patching "99" gave # node at $.from differes from replacement from:
```

An overlay is re-applied to a baseline expected to have changed, so checked forms cannot
appear in it. §3.1 bounds the exposure to one snapshot interval; it does not remove the
requirement.

**R2 — the two states must be key-annotated before diffing.** `diffArray` takes its keyed
branch only when **both** sides carry `!key(f)`, and materialized state never does, so the
keyed case came out positional and landed the scope's element by index. Solved without
touching `Diff` — see §3.5.

**R3 — ownership is the index's paths, not the diff's.** A minimal diff records only where
the two states *differ*, so a scope writing the value baseline already holds records
nothing and silently loses ownership:

```
baseline {a: {x: 5}} ; scope writes {a: {x: 5}}    → overlay: <no difference>
baseline {a: {x: 99}}  → via overlay: {a: {x: 99}}    via replay: {a: {x: 5}}
```

Not exotic: a controller reconciling by writing the value it already sees does this every
pass. The scope's **index paths are the ownership set** — the one consumer that makes the
per-path index earn its keep — so an owned path gets an entry even when the value
coincides.

### 3.5 Keying: annotate, don't re-plumb `Diff`

Under a schema-authoritative rule (P1) stored state stays op-free, so `diffArray` cannot
key itself. `doDiff`/`diffArray` carry **no path** through the recursion, so a path-keyed
schema cannot be consulted inside them without changing `DiffFunc` across `libdiff`.

It does not need to be. The overlay builder tags the two states from the schema before
diffing — legitimate exactly where *storing* the tag is not, because the overlay is a
write, and writes are where ops live. **Measured** (`scope_overlay_keyann_test.go`):

```
baseline@T (as stored, op-free): {items: [{q:1 sku:W}]}
scoped@T   (as stored, op-free): {items: [{q:1 sku:W} {q:3 sku:G}]}

overlay WITHOUT annotation: {items: !arraydiff {1: !insert {q:3 sku:G}}}   ← positional
overlay WITH annotation:    {items: !key(sku) [!insert {q:3 sku:G}]}       ← by key
```

Applied over a baseline that has since added its own `S`: `{W, S, G}` from the overlay,
`{W, S, G}` from replay — exact match, order included.

Entries are therefore addressed by **kinded path including key segments** —
`items("GIZMO")`, never `items[0]`.

**Non-keyed arrays are atomic.** No identity to address by, so the overlay holds the whole
array as one entry and does not use the `arr[0]`, `arr[1]` paths `indexPatchRec`
generates. Stated consequence: a scope writing one element takes ownership of the whole
array, frozen at `T`.

---

## 4. Prerequisites

### P1 — single-sourced key derivation

**Measured, blocking** (`key_routes_test.go`). Keyed-ness is decided per *write*, from the
tag that write happened to carry, so one array is indexed under both shapes:

```
after tagged write:    [items  items("a")  items("a").name  items("a").v]
after untagged write:  [items  items("a")  ...  items[0]  items[0].name  items[0].v]
```

An overlay addressed by index path then has two keys for one element and no ordering
between them.

The two routes are **disjoint**, not merely inconsistent. `api.Schema` holds only
`AutoIDFields` and `LookupKeyField` is a view over it, so a schema can only say "keyed" as
a side effect of "auto-generated" — a client-supplied `!key(name)` is *unsayable* in
schema. Meanwhile the auto-id route injects the id field but no `!key` tag
(`tx/auto_id.go`), so the index records `items("<id>")` from a patch that cannot be
navigated by that path — the gap `5hmq80f3h12krh1mbsn0` recorded as "remaining, unfixed",
inherited from `hbn7ptxch12krs778smg`.

**Proposed: schema authoritative.** Extend `Schema` past `AutoIDFields` to express keying
generally; index and apply both consult the active schema; a disagreeing tag is rejected.
State stays op-free, keying is declared, and the existing schema-migration machinery
carries changes.

Rejected — persisting `!key` on state: it embeds a schema statement in every instance, has
no declaration point and no migration path, and any writer can plant a different tag, so
the key space still forks — permanently and invisibly.

Notes: `index.Build` passes `nil` schema and relies on tags, so it needs the schema **at
that commit** — schema replay has to interleave with index build.
`SchemaResolver.GetSchema(scopeID)` is per-scope, so two scopes can key one path
differently; needs a rule (inherit baseline keying, or reject divergence).

**Migration: decided — none. Existing stores are discarded.** Changing key derivation
changes what `Build` produces, while a persisted `index.gob` holds paths derived the old
way and `persistedIndexStale` compares only log-file *generation*, not derivation rules —
so an upgraded store could load an index whose paths no longer match a rebuild, silently.
Rather than version the index or force a derivation-triggered rebuild, stores are
recreated. That is what makes P1 a contained change instead of a compatibility surface,
and it is only acceptable while scopes are sandbox-lifetime and the store is not yet
carrying anything anyone needs to keep.

### P2 — keyed coverage

`!key` appears in one storage test and none in the server tests. Each of these is
load-bearing and untested: identity merge at baseline (same key patched twice); a keyed
array through compaction; index rebuild from a log of keyed patches (`build.go`'s stated
contract); `patchMayAffect`'s op-tag fall-through, which the scoped watcher's soundness
argument rests on; watch deltas for a keyed change.

The keyed diff branch goes live on the scoped watch path once §3.5's annotation is in
use. Five defects sat there undetected until `9c5adc9..ad36016` precisely because it was
unreachable. P2 is what stops that being a regression.

**Write these to survive P1.** Four of the five pin semantics that hold under either key
derivation. The rebuild one does not: today's contract is "a rebuilt index reads `!key`
tags from the patches", and P1 replaces that with "a rebuilt index reads the schema at
that commit". Pin the property that survives both — *a rebuilt index agrees with the live
one* — rather than the mechanism, or P1 arrives having to rewrite the test that was
supposed to guard it.

### P3 — unconditional primitives, as a post-pass

**Measured** (`scope_overlay_uncond_test.go`): this needs no change to `Diff` either.
`libdiff.MakeDiff` is the only source of `!replace{from,to}`, and its branches are
`from == nil → !insert`, `to == nil → !delete`, otherwise the checked replace. So R1 is a
walk over the finished overlay rewriting each checked replace to the value it would have
installed:

```
overlay as Diff produces it: {a: {x: !replace {from: 1  to: 5}}}
after the post-pass:         {a: {x: 5}}
```

Verified both against a baseline that moved at the owned path (the case that errored
outright) and against a type change (`{a: {x, y}}` → `"scalar"`), each matching replay.

So the overlay builder is `annotate → Diff → rewrite → union owned paths`: a pre-pass and
a post-pass around an unmodified `Diff`. Nothing in `libdiff` or `diff.go` has to grow a
config or a path parameter. Should `!strdiff`/`!arraydiff` appear in overlays later (they
cannot today — §3.5 keys arrays, and strings diff to a checked replace), the same post-pass
is where they get flattened.

### P4 — scope enumeration

There is no way to list live scopes: `activeScopes` was excised with the old
scope-snapshot code, and `DeleteScope` is the only lifecycle signal. Phase 1 needs this on
the snapshot path, not just at compaction — `createSnapshot` has to know which scopes to
build an overlay for. Scanning the index for `ScopeID != nil` is the obvious source; a TTL
or last-write timestamp is the open question (§7).

---

## 5. Phase 1 — overlay at snapshot time

**Goal: all of §1.** Nothing is dropped, nothing is broken; the scope layer simply gets the
same snapshot treatment baseline has.

1. **P1** — schema keying; `index.Build` schema-at-commit; per-scope divergence rule.
2. **P2** — keyed coverage, written against today's replay layer so it guards rather than
   describes.
3. **P3** — `Diff` restricted to unconditional primitives.
4. **P4** — scope enumeration for the snapshot path.
5. **Overlay build**, in `createSnapshot`: per live scope, materialize baseline and scoped
   state at `T`, key-annotate both from schema, `Diff`, union with an entry per owned index
   path (R3). One materialization per scope per snapshot, replacing one per read and one
   per watch event.
6. **Overlay read** — `readScopedStateAt` takes overlay(T) + scope patches above `T`. Fix
   the `LookupRange("", nil, ...)` waste (§1) here.
7. **Scoped stepped head** — with the overlay as base, `MatchStateAt` can serve a scope the
   way `headStateAt` serves baseline, taking the CAS write off the O(N) path inside
   `commitMu`.
8. **Steppable scoped watch** — §8.

Steps 1–4 are independently useful and land without committing to the overlay.

**Suggested build order, which is not the list order.** P2 first: no risk, needed by
everything after it, and it is the guard that would have caught the five keyed-diff
defects. Then spike steps 5–6 **on non-keyed data**, where P1 is not required — the
overlay builder needs only §3.5's post-pass and R3's owned-path union, both small — with
the §8 differential as the acceptance test. That validates the construction end to end,
including the log-entry and index mechanics nothing has exercised yet, before P1's larger
surface is paid for, and it is cheap to throw away. P1 and P4 follow; stepping (7–8) is a
separate measured increment, because if it does not work the rest still delivers.

P2 and the spike are independent and can proceed in either order or together. The spike
lives in the worktree as ordinary code rather than in a scratchpad, so the differential
harness it produces is the same one §8 keeps.

**Storage form.** The overlay is a **patch entry** carrying `ScopeID`, indexed via
`IndexPatch` — not a snapshot. **Measured** (`scope_indexloss_test.go`): a snapshot is
indexed at the root only (`createSnapshot` sets `KindedPath: ""`), and compaction removes
every below-root copy of what it drops, so path-level entries go 5 → 0 and watch replay
loses its only path-level consumer. As a patch entry, path indexing survives — and R3
needs those paths.

Two traps: the overlay must span `StartCommit != EndCommit` or `patchNodesFromSegments`
skips it as a snapshot; and it must sort before any retained later scope patch, which
`[0, T]` gives for free.

**What phase 1 does not fix.** Scope patches still accumulate, still get rewritten by every
compaction, still pin their log segments until `DeleteScope`. Phase 1 takes the semantic
cost of §3.2 and buys speed with it, not disk. If the freeze turns out wrong for a real
workload, that is the exposure.

**And what steps 1–6 alone are worth.** They bound every cost in §1 to *one snapshot
interval*, which is the difference between unbounded and bounded and is the whole problem.
But that is parity with baseline only on the **read** path. Baseline's other two hot paths
are not bounded by the snapshot at all — they are O(patch), because `head.go` steps a head
document for CAS preconditions and `session.go` steps a watch document per event (§1). A
scope with an overlay and no stepping still pays a snapshot-interval replay on every
conditional write and every watch event, where baseline pays a patch.

So steps 7–8 are **parity work, not a bonus increment**, and the ~7600× in §1 lives there.
They are also the one part of this plan with no measurement behind it: `head.go` records
why a scoped view cannot be stepped today — a scope's writes apply last and shadow
stickily, so folding a baseline patch into a materialized scoped document lets baseline
overwrite a leaf the scope owns — and the claim that an explicit ownership set fixes that
is so far an argument. The mitigation is that both are ports of machinery that already
exists and is already load-bearing for baseline, not new mechanisms.

---

## 6. Phase 2 — compaction drops what the overlay subsumes

Once an overlay at `T` exists, the scope patches at or below `T` are redundant for
**reads**. They are not redundant for **replay**, which is what phase 2 has to deal with.

1. **Per-scope replay floor.** `droppedPatchFloor` excludes scope patches because none are
   ever dropped. Once they are, a scoped watcher resuming above the store-wide floor but
   inside a subsumed span gets the surviving subset and a success return — the silent-loss
   failure the floor exists to prevent, since `ReadPatchesInRange` cannot tell "no scope
   patches in range" from "nothing changed". Same persist-before-destroy ordering
   `raiseReplayFloor` uses.
2. **Divergence policy.** If an overlay is ever applied to a baseline that has moved at a
   path it asserts, say so rather than resolving it silently. The general form is a
   **read-set validation** — record what baseline held at the paths the overlay depends on,
   and fail the scoped read if baseline has diverged there — with a named error and a
   documented recovery, following `ErrReplayCompacted`'s precedent. Note that R1's checked
   `!replace` is *not* this: its coverage is incidental (it catches only replaced-and-moved
   paths, not the `!rename` case, not R3), and its message is a raw diff dump.
3. **Selection.** `selectSurvivors` stops exempting scope patches unconditionally and drops
   those at or below the scope's overlay anchor, subject to the floor.

Deferring all of this to phase 2 is what lets phase 1 ship without a "known broken"
window: the breakage worth budgeting for comes from dropping patches, and phase 1 drops
nothing.

---

## 7. Open decisions

**Freeze semantics.** §3.2. Baseline's existing bargain makes *the past* approximate; this
makes *the future* of an old scope write frozen. Defensible — arguably what "the scope owns
that path" always meant — but it is a decision, not an inheritance.

**Expansion granularity.** `!rename a→b` where `a` is a large baseline subtree freezes a
copy of it into the scope's ownership; later baseline edits under it stop showing through.
A one-line scope write can quietly capture a lot of baseline.

**Snapshot cost per scope.** Phase 1 materializes each live scope's state at every
snapshot, so `SwitchDLog` grows with scope count. Fine for a handful, needs thought at
hundreds. Interacts with liveness below: a scope nobody is using still costs a
materialization per snapshot.

**Scope liveness.** No TTL and no last-write timestamp, so an abandoned sandbox pins its
overlay and its patches indefinitely, and costs a materialization per snapshot. For the
sandbox use case a missed `deleteScope` is permanent.

**`!pipe` re-executes on every read.** `RejectUnsafe` defaults false and nothing in logd
sets it, so an unsafe op stored in a scope patch is re-run by every replay. The freeze
makes it execute once per snapshot interval instead of once per read. Worth its own issue
regardless of this work.

---

## 8. How it gets checked

**Differential against the oracle.** The replay implementation is correct and slow; keep it
as the oracle. For generated interleavings of baseline and scope writes, assert the
overlay-based read equals the replay-based read at every commit — except across a snapshot
boundary where a relative op froze, which the generator should mark rather than avoid.
This makes every behaviour in §2 a consequence instead of a checklist.
`scope_overlay_diff_test.go` is the seed.

**Property, not inspection.** Every defect found in the keyed diff path produced output
that *encoded* correctly and differed only in a tag, in how a key was spelled, or in a
field the merge dropped. Reading them passes; applying them does not. The generator and
comparison in `diff_patch_property_test.go` extend directly here.

**Watch equivalence.** For the same interleavings, a scoped watcher's delivered deltas,
folded from its initial state, must equal the scoped read at each commit — the property
that lets step replace recompute rather than an assumption that it can.

---

## 9. Payoff

Bounded replay is the smaller half, and on its own it only brings the read path level with
baseline. The larger one: the overlay makes scope ownership **explicit** — owned paths,
values, tombstones — which is what a scoped view needs in order to *step*: apply the
baseline delta, then re-assert the overlay over the touched region, so a baseline write to
a path the scope owns is suppressed rather than folded in.

That is not a new idea to invent for scopes. It is what baseline already does on both of
its hot paths, and the measurements in §1 are what it bought there: a conditional write
that stays at O(patch) while a cold read of the same state grows to 7.4 ms, and a watch
event at ~1 µs against a full recompute. Scopes are simply the case that never got it,
because until there is an explicit ownership set there is nothing to re-assert.

It only works if the layer holds no re-evaluating ops. Which is §3.2, and why that decision
comes first.
