# A path says how much of its history to keep: the plan

The plan for `!logd-compact(<duration>)`, in the form of rebuild_plan.md and scope_plan.md:
what the store does today and the facts this must keep, the one idea, why it is correct
against reads, writes and watches, and the phases that build it, each held by tests and
closed by an issue. It is a plan for:

    r482sz5rh12ksj0cvdn0   a record overwritten often keeps its whole history for the
                           store-wide cutoff, and neither retention nor compaction can
                           say otherwise                                (this plan)
    m56xswgsh12ks9ravdn0   a few-byte commit spends 0.5-0.9s in apply while 25-minute
                           snapshot+compactions run back to back        (not this plan;
                           see "What it does not do")

The issues are not to be linked, commented or closed from this plan. They are handled when
the work has results to show.

This is the third draft. The first was reviewed against the code; the second answered
the consumer's comments on r482 of 09:42 and 10:04 (what verse reads at past commits,
and where its records sit); this one answers the comment of 10:23, which withdrew the
ask for a loud failure at a declared path and proposed that a past read there answer
the current value. What each changed is marked "(review)" or "(10:23)" where it
matters, so the reasoning is checkable.

Read first, in this order:

    compaction.go                      the walk, selectSurvivors, the two floors it raises
    scope_compaction.go, index/cover.go, index/footprint.go
                                       dominance as built for scopes; this plan reuses it
    replay_floor.go                    what the floors promise and to whom
    cursor.go (Read, provenAbsent, openRead)
                                       what a read folds, and the proof of absence the
                                       floor disarms
    lower.go                           why a stored write is absolute, for baseline too
    snap_storage.go (SwitchDLog)       the switch: snapshot into the inactive log, then compact
    index/cursor.go (Segments)         what a read or replay at a path folds
    api/schema_parse.go, api/schema.go how the schema's logd tags become paths
    ../../../../docs/logd/compaction.md   the contract today, "What cannot be configured"
    git issue show r482sz5rh12ksj0cvdn0   the observation and the comments
    git issue show cpqj2tf6h12kr5jxqxn0   how a read says it was answered approximately

---

# Part 1. The algorithm, and why it is correct

## Terms

- **Statement.** What one log entry says at one path: a segment with `Statement` set,
  carrying a cover (`index.Cover`): what it *offers* to the statements before it and
  *needs* from the ones after. A patch entry has a statement at every path it states
  something at and a spine segment at every path it merely passes through
  (`index.EachSegment`). A plain object is spine: `out: {delivered: 1, failed: 0}` is
  not a statement at `out`, its two leaves are the statements (log_segment.go,
  `passesThrough`).
- **Dominated.** A statement is dominated by a LATER statement that covers it: a total
  cover at or above its path, a whole cover strictly above it, or a whole cover at its
  own path when it needs only whole (`Cover.dominates`). A dominated statement
  contributes nothing to any fold that includes the dominating one. This is sound
  because a stored write is absolute (`lower.go`): it states what a value is, not how to
  change it. What offers and needs what (cover.go, `Classify`): an untagged, uncommented
  scalar offers whole and needs whole, so a later plain scalar at the same path retires
  it; a scalar with a data tag or a comment needs total, which only `!insert` or
  `!delete` at or above it offers; `!replace` and the other operators offer none.
- **An entry is dominated** when every statement it makes is dominated.
- **Declared path.** A schema path whose define carries `!logd-compact(<d>)`. `d` is the
  path's own cutoff; `0` means no history past the current value. A path is *under* a
  declaration when the declaration is at it or at an ancestor of it, in the schema's
  terms: keyed element names elided (`schemaPath`), and a `*` segment of the declaration
  standing for any one field.
- **Declared entry.** A baseline patch entry every statement of which is under a
  declaration. An entry with one statement outside any declaration is not declared,
  whatever else it states.
- **The declared mark.** One persisted commit: the highest of any entry compaction has
  dropped by the declared rule. Written before the drop, like the store's floors, and
  read by one thing, the proof of absence (F6). It is not a floor: no read or replay is
  refused or moved by it. (review, 10:23)
- **The files.** Two logs. Writes land in the active one. At a switch (`SwitchDLog`) the
  active file becomes inactive, the root snapshot of the state at the switch commit `S` is
  appended to it, and it is compacted. The other file is active until the next switch.
  A file holds, in position order, which is commit order (`CompactInactive` rewrites
  survivors in ascending position; a switch does not empty the file it switches into):
  the survivors of its earlier compactions, then the writes of its last active epoch,
  then its snapshot and any path snapshots taken since. It is never compacted while
  active.

## Facts the store already holds

These are what the argument below rests on. None is changed by this plan.

- **F1.** A read seeks the newest snapshot at or above its path whose commit is at or
  below the commit asked, and folds the surviving writes after it that reach its path:
  statements at the path, at its ancestors, and at every path beneath it, each entry
  projected at the path (`openRead`, `index.Segments`: every entry indexed anywhere
  under kp is indexed at kp). A head read's base is the newest root snapshot or a path
  snapshot after it. (review: the first draft said a head read folds only the active
  file. Not quite: a write in flight across the switch lands in the old active file
  above `S`, and in the window between `SwitchActive` and `createSnapshot` the newest
  root snapshot is still the previous one. C1 rests on dominance, not on file
  placement.)
- **F2.** A read at a past commit is answered at the commit asked when the newest root
  snapshot at or below it is at it or at or above the baseline floor, else at the
  **ceiling**: the oldest root snapshot at or after it. The result carries the commit
  answered, and a not-found carries it too, so a client that asked for `C` and was
  answered at another commit can tell. One rule, independent of the path: a commit
  answers alike at every path (`AnsweredCommit`; cpqj2tf6h12kr5jxqxn0, decided).
- **F3.** A watch resuming from commit `c` replays the surviving patch entries in
  `(c, head]` that reach its path, in commit order across both files (`Segments` merges
  per node by commit), then follows live. A replay always runs to the head. It is
  refused below the replay floor; with `noInit` it is refused when the state at `c` is
  not held exactly; with an initial state it is moved to the commit that stands for `c`
  (`Deltas`; session_watch.go).
- **F4.** Compaction removes dropped entries from the index before the file is
  rewritten and re-indexes survivors after, so reads and replays never see a dropped
  entry (`Compact`).
- **F5.** The floors are one number each for the whole store. The replay floor is the
  highest commit of any dropped patch; the baseline floor the highest of any dropped
  baseline patch. (`replay_floor.go`.)
- **F6.** The index proves a path never written from its skeleton alone — no node under
  a written object — and answers absent without opening the log (`provenAbsent`,
  `UnwrittenBelow`). It is disarmed once the baseline floor is above zero, because a
  dropped patch takes its segments with it, and after a reopen the index has no node
  for a path whose only writes were dropped, though its value is in a snapshot
  (f43dnqpkh12kr1kxqxn0). (review)
- **F7.** The scope rule: a scope's entry beyond the cutoff goes when it is dominated
  within its scope; it counts toward the floors as a baseline drop does.
- **F8.** The precondition of a write, lowering, retention and the snapshot's
  `patchesSince` all read at the head or at the epoch just closed, never across a
  compacted range (precondition.go, commit_ops.go, retention.go, snap_storage.go).

## The one idea

Treat a declared entry the way a scope's entry is treated — droppable when dominated —
with the path's own cutoff in place of the store's, and **without moving the store's
floors**. No read is refused or moved for it, and no read changes: a past read at a
declared path folds what survives, which the declaration says is enough. Everything
else about compaction stays as it is.

## The algorithm

Compaction of the inactive file, given the active schema's declarations `D`: a map from
schema path (with `*` segments) to duration. When `D` is empty nothing below runs and
the pass is today's.

1. **Walk** the file once in position order, as `compactionRecords` does. For each
   baseline patch entry (`ScopeID == nil`, `Patch != nil`), the kept ones included (a
   schema commit's rewrite is a total cover at its arrays and dominates like any
   statement): derive its statements (`index.EachSegment`). Translate each statement's
   path to the schema's path. If any statement is not under a declaration, the entry is
   **not declared**. Otherwise it is declared: record the longest duration among the
   declarations its statements fall under (its *wait*). Either way, feed every statement
   that IS under a declaration, in walk order, to a **dominance set**: an
   `index.Footprint` under one pseudo-scope, holding baseline's declared statements for
   this pass alone. Feeding a statement retires the earlier statements it dominates, so
   when the walk ends the set holds, per declared leaf, the newest statement in this
   file and no other (`Footprint.state`; statements of one entry never dominate each
   other, since `state` compares commits strictly).

2. **Decide.** A declared entry is dropped when (a) none of its statements is live in
   the dominance set, and (b) its timestamp is older than `now - wait`. Call this a
   *declared drop*. Every other entry is decided as today: a baseline patch goes beyond
   the store's cutoff, a scope's when dominated beyond it, root snapshots by the tiers.
   The declared rule is asked **first**: an entry that is a declared drop is one even
   if it is also beyond the store's cutoff.

3. **Floors.** `droppedPatchFloor` and `droppedBaselineFloor` are computed over the drops
   **excluding declared drops**; the store's floors do not move for them. The declared
   mark rises to the highest commit among the declared drops, and is persisted
   **before** the destructive step, as the floors are and for the same reason: a crash
   in between leaves it too high, never too low.

4. **The rest is unchanged.** Unindex the dropped, rewrite, re-index the survivors.

### What survives, per declared leaf

In any one file, after a pass: the newest statement at the leaf in that file, plus
every statement younger than the leaf's wait. Across the store: the newest statement at
the leaf overall is either in the active file, which is never compacted, or is the
newest in its file, which nothing in that file dominates, and survives. So **the newest
statement at every declared leaf is always on disk.** This is the property the
correctness argument uses, and it is why the rule is dominance and not age alone: age
alone would drop a leaf's newest statement once it was old enough, and C3 below would
fail.

A leaf's statement that is the newest in its file but not overall waits one more
round: the next compaction of that file sees the later statements appended to it since
and drops it. The lag is one statement per leaf per file.

A leaf that is never dominated is never dropped by this rule. That is the case for a
leaf written with a data tag or a comment, which needs a total cover: a record whose
leaves are tagged values accumulates under a declaration exactly as it does today,
unless the writer sends the record as `!insert` or deletes it. (review) This is stated
to the writer in the docs rather than worked around in `Cover`, whose rules are the
scope rule's and are not this plan's to loosen.

### What a past read shows at a declared path

A read at a past commit `at` is answered as today (F2): at `at` when the baseline floor
allows, else at the ceiling, and it folds what survives (F1). At a leaf under a
declaration, beyond the leaf's duration, the fold shows the value of the newest
surviving statement at or before `at`, or the base snapshot's value when none survives
in between, or **absence** when the leaf's first write is after the snapshot and was
dropped. Each is a state the leaf was in at some commit at or before `at`; none is
promised to be the state at `at`. Within the duration the leaf's statements are all
kept and the read is exact there, as everywhere.

This is the quiet answer, chosen over two others (10:23):

- **A loud failure** — refusing, or answering at the ceiling with the commit reported,
  for a read at or under a declaration — was asked for at 09:42 and withdrawn at 10:23:
  it guards against a wrong declaration only, and it costs every read that *covers* a
  declared path — a listing of `iods` at a past commit, a firing's subtree read, a
  watch's initial state — a failure for one field inside it. The second draft built
  that with a floor per declaration; it is gone.
- **The current value** — a read at any past commit shows the declared leaf as it is
  now — is what 10:23 proposes, and it is the cleaner sentence for `0`: a path declared
  at `0` has no past. It is not what the fold gives. To give it, a read at `at` would
  also walk the entries in `(at, head]` that reach its path, project each at the path
  and prune the projection to the declared subtrees, and fold those on top — on the
  staging store that is thousands of decodes for a past read of `verse`, paid by every
  such read whether or not it cares for the declared leaves, in the hottest path the
  store has, for the benefit of a path nobody reads at a past commit by the rule that
  got it declared. And for a duration other than `0` the read would need the commit's
  age to know which rule applies, which the index does not carry.

10:23's objection to the quiet answer stands and is accepted as the cost: a leaf born
inside a dropped run reads absent at a commit where it stood. What 10:23 proposed in
its place — "beyond the duration, the oldest surviving statement" — is not what
survives: at `0` the survivors are the newest per file, so the oldest surviving is the
previous file's newest, not the current value, and the fold's natural answer is the
newest at or before `at`. The contract is therefore stated as the fold, not as a
rule the fold would have to be bent to.

## Why it is correct

**C1. The current value is exact at every path, declared or not.** A head read folds
the surviving statements after its base (F1). Every dropped statement has a later
surviving statement in the same file that dominates it, and a dominated statement
contributes nothing to a fold that includes its dominator; a fold from the base to the
head includes every later statement. So the fold of the survivors is the fold of all.
(review: this is the argument, not "the inactive file is never folded at the head".)

**C2. Nothing a non-declared path relies on is dropped by the new rule.** A declared
drop states only paths under declarations. A read at a non-declared path folds
statements at it, above it and beneath it (F1). Above it: a statement at an ancestor of
a non-declared path is under a declaration only if the declaration is at or above that
ancestor, in which case the path is under it too. Beneath it: a non-declared ancestor of
a declared path folds the declared subtree's survivors and sees there what "What a
past read shows" says. Outside the declared subtree it is exact as today.

**C3. A watch never ends stale.** A watch resuming from `c` above the replay floor
replays the surviving entries in `(c, head]` (F3). At a declared leaf, the newest
statement overall survives (above). If it is in `(c, head]` the replay delivers it,
last among the leaf's statements, and the watcher holds the current value. If it is at
or before `c`, no later statement exists, and the watcher's state at `c` holds it: a
`noInit` watcher holds the true state at `c` itself — every dropped entry's dominator
is strictly later in the same file, so in range, and lands on top — and a watcher that
took its state from the store took the fold at `c`, which at the leaf is a state at or
before `c` (above), and the replay delivers every surviving statement after `c`, the
newest among them. Either way the watcher ends at the current value at every leaf, and
the sequence it saw at the leaf is a subsequence of the true one ending at its last
element. The values it did not see are the history the declaration gave up.

The replay floor exists to make a *silent* gap loud (replay_floor.go). This gap is not
silent: it is declared, in the schema, by the writer, and C3 is what the declaration
promises in its place. That is why the store's floor does not rise, and why nothing
new refuses a watch.

**C4. A read at a past commit.** Answered against the baseline floor alone (F2), so the
answered commit is what it is today. At every path not under a declaration it is exact
when it says it is (C2). Under a declaration it is "What a past read shows": a state
the leaf was in at or before the commit, quietly. This is the one place the bargain is
quiet, and it is the bargain: a declaration says nothing reads the path as of a past
commit, and a read that covers it reads bookkeeping.

**C5. Preconditions, lowering, retention, snapshots.** All read at the head or at the
epoch just closed (F8). Unaffected.

**C6. Scopes.** A scoped read folds baseline to the commit asked for and then the scope's
live statements (`openRead`; its from-zero walk skips baseline segments). Its baseline
term is C4. Its scope term is untouched: scope entries are not declared entries, and
the dominance set holds baseline statements only.

**C7. Transactions and mixed entries.** An entry with any statement outside a declaration
is kept whole (step 1). Nothing is rewritten inside an entry. verse's registration entry,
which states `name`, `url`, `owns` and three fields of `out` together, is such an entry
(10:04); its per-pass entry, which states only `out.delivered`, `out.failed`,
`out.lastPass` and `out.lastError`, is a declared entry, and its `lastError: !delete`
on success is a total cover that retires an earlier error object.

**C8. Schema changes.** Declarations are read from the **active** schema at each pass. A
path declared later: its older entries become droppable at the next compaction of their
file. A path undeclared later: nothing more drops; what was dropped stays dropped, and
reads there show what survives, as they did while it was declared. A schema commit
that changes only declarations carries no rewrite and ends no watch.

**C9. Crash safety.** The pass is today's sequence (F4) with one more number written
before the destructive step, by the rule of `raiseReplayFloor`. The dominance set is
rebuilt from the file each pass and is not persisted.

**C10. Reopen and rebuild.** An index rebuilt from the log sees fewer entries, as after
any compaction; the schema history comes from the kept schema commits and the root
snapshots' schema entries, untouched. The declared mark lives in `meta/` beside the
floors, not in the index, for the reason given there: an index discarded and rebuilt
must not take the record of a drop with it. With it persisted, F6's proof of absence is
disarmed on reopen as the baseline floor disarms it today, which closes the case the
review found: a path whose only writes were declared drops, read as never written
after a reopen with its value in a snapshot. Without it the answer would depend on
whether the index had been reopened, which index_residency.md forbids.

## Cost

Per pass, with `D` non-empty: `EachSegment` over each baseline patch entry in the file —
CPU over entries the walk already decodes — and a dominance set whose size is the live
statements it holds, about one per declared leaf per file (dominated statements leave as
they are dominated). This keeps compaction's invariant that it holds one record per
entry and never the index's view of every segment (`compaction.go`, index_residency.md).
The mark: one commit. Reads: unchanged. With `D` empty and no mark: nothing.

## What it does not do

- **It does not change the snapshot cadence or what a snapshot costs.** A snapshot is
  triggered by `maxCommits`/`maxBytes` and takes what it takes; that is
  m56xswgsh12ks9ravdn0. Every overwrite is still a commit, pays its apply and counts
  toward the next snapshot (09:42). What this changes is what the compaction after the
  snapshot rewrites: on the staging store, the overwrites of the declared paths go
  instead of 1.5% of the log, provided their leaves are plain scalars.
- **It does not touch the active file.** The record of the last epoch is there in full.
- **It does not lengthen history.** Beyond the store's cutoff a declared path's entries
  go as every path's do, by the ordinary rule, floors and all. A declaration longer than
  the store's cutoff changes nothing, and is not refused: the schema cannot see the
  store's config.
- **It does not apply to scopes**, which have their own rule.
- **It does not change what a read does, or says.** The answered commit is today's; the
  body at a declared leaf is what survives.
- **It does not retire a tagged or commented leaf.** See "What survives".
- **It is not for a path a rule reads at a past commit.** A schedule's tick or a delivery
  status is read at its causing commit, hours later after a gate (10:04); declaring one
  makes those reads quietly stale. The paths verse would declare are the iods' `out`
  records, at `0`.

---

# Part 2. How to get there from here

Five phases. 0 and 1 are inert — they parse and validate and change no behaviour — and
land first. 2 makes it live, mark included; it does not split, because a declared drop
without the mark is the review's finding. 3 holds it. 4 says it. Each phase is one issue
and one commit series; nothing in a later phase is needed to ship an earlier one.

## Phase 0: shared helpers find their package

- `server.ParseDuration` (fileconfig.go) reads `1h`, `1d`, `2w`, `1y`. The schema tag's
  argument needs it in `api`, which `server` imports, so it moves to `api.ParseDuration`;
  `server/fileconfig.go` and `server/retention.go` call it there. `api` imports neither
  `server` nor `storage`, so this is clean.
- `schemaPath` (server/retention.go), which elides keyed element names to give a document
  path's schema path, is needed by storage in phase 2. It cannot go in `api`:
  it uses `ident.Parse`, and `storage/ident` imports `api` (review). It moves into
  `storage/ident` as `ident.SchemaPath`; its callers are `server/retention.go` and
  `server/session_read_set.go` (review: the second was missed).

Held by: the existing tests of the callers. No behaviour change.

## Phase 1: the schema knows its declarations

`api/schema_parse.go`:

- A new tag constant `compactTag = "!logd-compact"`. `walkDefine` recognizes it on a
  field's value with `ir.TagHas`, which matches the head of `!logd-compact(0)`, reads
  its arguments with `ir.TagGet`, which answers `["0"]`, and records a
  `CompactPath{Path: kpath.ChildField(parentPath, fieldName), Within: d}`. It then
  **continues into the value** as it does for any object, so a `!logd-key` beneath a
  declaration is still found.
- **A `*` segment.** `define:` is a path tree, not a set of types: a declaration's path is
  the literal keys above it, and verse's records sit under a key that is whatever iod was
  installed (10:04). A field named `*` in `define:` stands for any one field of the
  object: `walkDefine` steps into it with `kpath.ChildField(parentPath, "*")`, so a
  declaration at `verse: verse: iods: "*": out:` has the path `verse.verse.iods.*.out`.
  A literal field named `*` cannot be declared; the docs say so. Keying gets the same
  step for free in `walkDefine` but `Identity` still matches by equality; teaching it
  `*` is not this plan.
- `Schema` gains `Compact []CompactPath` and `ParseSchemaFromNode` answers non-nil when
  any is present.
- `Schema.Validate` refuses: a `!logd-compact` with no argument or more than one — `ir.TagGet`
  answers `[]` for both `!logd-compact` and `!logd-compact()`, and both are refused —
  an argument `ParseDuration` does not read, and a negative one. `0` is accepted and
  means zero. Nested declarations are allowed: the nearest wins.
- `Schema.CompactWithin(schemaPath string) (within time.Duration, ok bool)`: the
  nearest declaration at or above the path, matching segment-wise with `*` for one
  field, and its duration.

`storage/schema.go` (`SetSchema`) already parses and validates on adoption; a refused
declaration is a `SchemaRefusedError`, named, before the commit is taken.
`tschema.ParseSchema` stores define values verbatim and checks only definitions
reachable from `accept`, so a tagged define value passes as `!logd-key`'s does today;
a schema whose `accept` reaches it fails "unknown tag in schema" for both, which is the
keying tags' pre-existing gap and not this plan's (review).

Held by: a table test over schema documents → declarations and refusals, in
`api/schema_parse_test.go`, extending the table the keying tags use.

## Phase 2: compaction drops declared entries

`storage/compaction.go`, `storage/replay_floor.go`, `storage/cursor.go`, and one exported
helper in `index`.

- `index`: export what the walk needs of the footprint for a throwaway set:
  `NewFootprint()`, `(*Footprint).State(scope string, st Statement) bool` wrapping
  `state`, and `StatementOf(seg *LogSegment) Statement` wrapping `statementOf`.
  `EntryLive` is exported already. No change to how the index feeds its own footprint.
- `compactRecord` gains `declared bool`, `wait time.Duration` and `declaredDrop bool`.
- `compactionRecords` takes the declarations (`s.schema.ActiveParsed()`, nil for a
  schemaless store). For a baseline patch entry it runs `index.EachSegment`; for each
  statement it asks `CompactWithin(ident.SchemaPath(seg.KindedPath))`; a miss clears
  `declared`; a hit feeds the statement to the pass's footprint under a fixed
  pseudo-scope and raises `wait`. Statements of non-declared and kept entries that hit
  are fed too (step 1 above).
- `selectSurvivors` asks the declared rule first for a baseline patch record:
  `r.declared && !foot.EntryLive(pseudo, file, r.pos) && r.timeOK && r.time.Before(now.Add(-r.wait))`
  → dropped, `declaredDrop = true`. Otherwise today's rule.
- `droppedPatchFloor`/`droppedBaselineFloor` take the records and skip declared drops.
- **The declared mark** (`replay_floor.go`): `meta/declared-mark`, eight bytes as the
  floors are, loaded at `Open`, raised by `Compact` before the destructive step by the
  rule of `raiseReplayFloor`, never lowered.
- `provenAbsent`: `s.baselineFloor.Load() > 0 || s.declaredMark.Load() > 0` disarms it.
- `AnsweredCommit`, `Deltas`, the session's read and watch: **unchanged**.
- The compaction log line gains `declared=<n>`.

Nothing else moves: unindex, rewrite, re-index, grace period, tiers, the floors'
meaning, the reads, the protocol.

Held by phase 3 before it merges.

## Phase 3: the tests that hold it

Following fewer-more-general: one differential and two unit tests, beside the existing
ones, not a test per case.

- **`TestDeclaredCompactionDifferential`** (storage/compaction_test.go), in the form of
  `TestScopeCompactionDifferential`: two stores, one schema declaring `cur` at `0`,
  `hour` at `1h` and `"*".out` under an open-keyed object, over a define that also has
  undeclared paths; the same generated stream of baseline writes — whole-record
  overwrites, partial ones, `!delete`, `!insert`, `!replace`, a tagged leaf and a
  commented leaf, a leaf first written mid-stream, a transaction stating a declared and
  an undeclared path in one commit, writes at undeclared paths, with switches scattered
  in it. The subject switches and compacts both files twice under a **long** store
  cutoff, so only declared drops happen. Asserts:
  - head reads agree at every path (C1);
  - the replay floor and the baseline floor are 0 on the subject, and the mark is at
    most the highest commit of a declared entry (step 3);
  - past reads at undeclared paths agree with the reference at every commit, answered
    at the commit asked (C2, C4);
  - at every past commit, a read at or under a declaration is answered at the commit
    asked, and at each declared leaf shows a state the reference's leaf was in at some
    commit at or before it ("What a past read shows");
  - at every commit `c`, a replay `(c, head]` on the subject applied to the
    **reference's** exact state at `c` yields the head state — the `noInit` client's
    case (C3, review) — and applied to the subject's own state at `c` likewise;
  - the entries at `hour` are all kept (younger than their wait); at `cur` the dominated
    plain-leaf entries are gone, the tagged and commented and `!replace`-written ones
    are kept, and the newest statement of each leaf is in some file;
  - **after a reopen**, with the manifest and with a full rebuild, every assertion above
    holds again, a narrow read of a path whose writes were all dropped included (C10,
    review: the case the first draft would have failed).
  Then, with the store cutoff made short, that entries beyond it go by the ordinary
  rule and the store's floors rise — the two rules compose.
- **`TestCompact_DeclaredNewestSurvivesTheFile`**: writes at a declared leaf across
  three switches; after each compaction the leaf's newest statement in each file is
  present and the older ones are gone, which is the one-round lag stated above.
- **`TestDeclaredMark_SurvivesReopenAndIndexLoss`**, beside the replay floor's: the mark
  is on disk before the rewrite, survives a reopen and a discarded index, and is never
  lowered.
- The schema table test from phase 1.

`everythingBeyondCutoff()`, `commitAt`, `readSubtreeAt` and `replayCommits` are reused.

## Phase 4: the docs say the contract

- `docs/logd/compaction.md`: "There is no per-path or per-scope policy for history"
  becomes the per-path section: the tag, the `*` segment, the contract (C1, C3 and
  "What a past read shows" in the reader's words), what a declaration is **not** for (a
  path read at a past commit), that a tagged or commented leaf is retired only by
  `!insert` or `!delete`, the one-round lag, that it shortens and never lengthens, that
  `0` is zero where the config's zero is default, and that it does not apply to scopes.
  The cutoff row of the knobs table and "Delta records are kept if ..." gain the
  exception.
- `docs/logd/keyed.md`: the `*` segment, since keying's `walkDefine` steps through it
  too.
- The sweep (sweep-a-corrected-claim-tree-wide), every site the review found that says
  history is exact within the cutoff or that a read never folds what is left:
  `storage/cursor.go` ("never as a fold of what is left" — true of the commit, now with
  the declared exception at the leaf), `storage/replay_floor.go` ("within the cutoff
  replay is exact to the commit"), `api/session.go` ("replays the exact delta history"),
  `docs/logd/session.md` (the watch's cutoff paragraph), `docs/logd/index.md` (the
  compaction summary, twice), `docs/articles/logd-document-store.md`, `api/doc.go`
  "Keyed arrays" (a sibling paragraph for history), `storage/doc.go` (the scope
  compaction sentence). "A property of the COMMIT, not of a path" in replay_floor.go
  stands: the answered commit is still the commit's.
- mkdocs.yml: no new page unless the section outgrows compaction.md.

## Phase 5: verse declares (another repository)

Not this plan's to do, but its reason: verse's schema declares `!logd-compact(0)` at
`verse: verse: iods: "*": out:`, and perhaps `lastReflect`; not the ticks, not the
`~status` records, not the registration's `lastError` (10:04). Whether its store has a
schema today is verse's to say; until it does, the mechanism is inert there.

## Open questions, for Scott before phase 2

1. **What a past read shows at a declared leaf.** The plan folds what survives and
   states that, with the absent-where-it-stood case named as the cost. The consumer
   (10:23) would rather it showed the current value; the cost of giving that is in
   "What a past read shows". If the current value is wanted, phase 2 grows a read-side
   overlay for declared subtrees and the plan needs a fourth draft for it.
2. **Tagged leaves.** If verse's `lastPass` is a tagged timestamp the per-pass entry is
   never dominated and nothing drops for it. The plan tells the writer rather than
   changing `Cover`; `Cover` could learn that a later tagged scalar at the same path
   dominates an earlier one, if that is sound for scopes too, which is a separate
   question.
3. **The `*` spelling.** A field named `*` in `define:`; or `!all` over a reference,
   which is how tonyschema types such an object and would need `walkDefine` to resolve
   references. The first is a dozen lines and matches how define is read today.
