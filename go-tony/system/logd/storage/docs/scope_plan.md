# A scope pays for its footprint, not its history: the plan

The plan for the scoped store's cost, written after wk5w1ddkh12krj1tkxn0 closed and in the
same form as rebuild_plan.md: what is measured, what the rebuild established that this must
keep, the one idea, and the phases that build it, each held by tests and closed by a number.
It is a plan for the three open scope issues and for the one issue under them:

    sb33w8p9h12kr16kg5n0   a scoped write rebuilds its view      (the write side)
    9b2vpggxh12ks0qde5n0   scoped reads cannot be stepped        (the watch side)
    4wpqh7t2h12ks1fvj5n0   one storable-delta pipeline           (superseded; see phase 3)
    mgg9nvt6h12krn6dksn0   a !raw subtree should MERGE           (the prerequisite)

The issues are not to be linked, commented or closed from this plan. They are handled when
the work has results to show.

Read first, in this order:

    SCOPES.md                          what a scope is, and the cost section at its end
    scope_compaction.go                the cover rules; this plan rests on them
    cursor.go (openRead)               where the scope's history is folded from commit 0
    path_snapshot.go                   why a snapshot is baseline's, and how a read prices one
    index_residency.md                 the durable index the footprint joins
    rebuild_plan.md, phase 7 AS BUILT  dominance as compaction built it
    git issue show mgg9nvt6h12krn6dksn0

## What is measured

On main at 153d44a (go-tony v0.0.207), `TestScaling_Reads` and `TestScaling_Writes` in
this package, cost of the Nth operation after N of the same:

                                              N=50     N=100    N=200    N=400
    baseline read at a path                   23us     30us     24us     27us     flat
    scoped read at the root, N scope writes   844us    1.72ms   3.79ms   7.45ms   linear
    scoped read, N BASELINE writes            65us     61us     66us     78us     flat
    baseline write                            718us    705us    756us    724us    flat
    scoped write, unconditional               2.26ms   4.13ms   7.88ms   17.8ms   linear
    baseline write, CAS                       1.45ms   1.0ms    1.1ms    1.05ms   flat
    scoped write, CAS                         6.5ms    6.45ms   ...               linear

Baseline is flat because of phases 3b and 7: a write is verified by a bounded read at each
site it states something at, and a read at a path folds only what changed under it since
its snapshot. The scoped rows are linear because a scoped read has no snapshot to fold from.

THE MECHANISM, in one place. `openRead` folds baseline from the nearest snapshot forward,
then the scope FROM COMMIT 0:

    for seg := range s.index.Segments(kp, &startCommit, &at, nil)   ... baseline since the snapshot
    for seg := range s.index.Segments(kp, nil, &at, scopeID)        ... the scope, all of it

A snapshot is baseline's only. `snapshotPath` reads with a nil scope and writes
`SetScopeID(nil)`; `latestSnapshot` filters scope segments out of the seek. So nothing
shortens the scope term, and every consumer of a scoped read pays it:

  - a scoped WRITE, through `stateAt(commit-1, scopeID, site)` at each site it verifies
    and at each precondition (sb33w8p9). Interleaving with baseline no longer matters,
    because the kept scope document that made a run of scoped writes flat went with the
    head in 3b. The cost is uniform now, and linear.
  - a scoped WATCH, which re-reads its view at the watched path per event that can reach it
    and diffs whole values (9b2vpggx, `emitScoped`). verse's scoped child store opens its
    watch at `verse`, so with a sandbox connected that is a scoped read of the whole verse
    subtree plus a whole-document diff, per commit. Unmeasured on staging; the shape is
    enough to know it is the largest scoped cost in the deployment.
  - compaction's dominance pass, which reads EVERY entry of a scope to decide which entries
    beyond the cutoff a later one dominates (`dominatedScopeEntries`).
  - `DeleteScope`, which is `removeAll` on every node of the trie, and `removeAll` pages the
    whole node in. Deleting a scope pages the entire durable index.

Phase 7's dominance is the one thing in the tree that bounds a scope's history, and it runs
only in compaction, only beyond the cutoff, and only by reading the entries.

## What this plan keeps

The direction since v0.0.204, which every phase is held to and which decides what is NOT
built below:

  - One read, at a path, answered by a cursor, bounded by what changed under the path since
    its snapshot (read_write_interface.md). The signature rule stays law.
  - The head is a number. Nothing materialized is kept for a scope or for baseline; there is
    no stepped document anywhere.
  - A snapshot is of a path, and the reads decide when one is worth taking.
  - What is stored is absolute (one_delta_shape.md). A scope stores the CLAIM it made
    (3e527ec), not the difference it made.
  - The durable index is the truth and the resident trie is a cache of it; a rebuild from
    the log is always available and always right (index_residency.md).
  - Every decline is counted, because a policy that refuses silently is one nobody can debug
    (153d44a).

## The idea

A snapshot of the scoped VIEW at a path cannot be stepped: the scope's writes apply last, so
folding a later baseline delta into a materialized scoped value lets baseline overwrite a
leaf the scope owns (9b2vpggx). A snapshot of the scope's LAYER needs a composition
primitive the format library does not have (rebuild_plan.md, 3a AS BUILT) and would recreate
the overlay that 73e2637 deleted as "a cache of a layer nothing can derive". Both are the
wrong object.

Phase 7 already proved the fact a scope actually needs. A stored scope write is absolute, so
a later statement at a path DOMINATES everything before it at and below that path, in a way
scope_compaction.go states precisely (the covers, below). Dominance needs no composition and
materializes nothing: the dominated entries are simply not folded, and the fold of what
remains is the fold of everything. That is a snapshot that writes no bytes:

    A SCOPE'S SNAPSHOT OF A PATH IS ITS LAST COVERING STATEMENT THERE.

What is missing is that only compaction knows it, and only by reading the entries. So the
index learns it. Per scope, a FOOTPRINT: a trie of the paths the scope has stated something
at, holding at each node the scope's LIVE statements there -- the ones no later statement of
the scope dominates -- each as (commit, log file, position, generation, need). A scoped read
at kp then folds baseline as it does today and, for the scope, the live statements on kp's
ancestor chain, at kp, and under kp, each entry once. The scope term of a read is bounded by
the scope's footprint under the path and not by its history, which is the property
sb33w8p9 asks for in its own words: "cost per scoped write proportional to the scope's OWN
footprint".

THE COVERS, restated from scope_compaction.go so this document can be read alone. What a
later statement at q says about everything at and beneath q:

    total   the result at q is the operand, or absence, whatever was there: !delete, and the
            replacing escape (!raw today; !insert and !insert.raw after the prerequisite)
    whole   the value at q is replaced and everything beneath it with it, but at q itself the
            merge may keep a comment or a tag the earlier statement carried: a plain scalar,
            or an array of plain scalars
    none    anything else: an object with fields is not a statement (its fields are), an
            empty object merges nothing away, an array with object elements merges element by
            element, and !addtag, !rmtag, !comment say something relative to the node

A statement NEEDS whole when it is an untagged, uncommented scalar or array of them, an empty
object, or a total cover itself; otherwise it needs total. A statement at q is dominated by a
total cover at or above q, by a whole cover strictly above q, or by a whole cover at q when
it needs only whole. Those rules were PROBED against the fold before they were written
(test_corpus.md, phase 7) and are not to be re-derived here.

THE FOOTPRINT, maintained at every scope commit where the entry is indexed: each statement
of the entry is added at its node; each statement that offers a cover drops what it
dominates -- at its own node by need, and everything under it. The work is proportional to
what is being dominated and is paid once, at the write, instead of at every read. It is
persisted in the manifest beside the regions, rebuilt from the log by Build exactly as the
segments are, and removed whole by DeleteScope. It is resident, and small: its size is the
live statements of every scope, which for a sandbox is its entities' leaves.

THE INVARIANT, in the words phase 6 used for residency: THE FOOTPRINT CHANGES A COST, NEVER
AN ANSWER. A scoped read served from the footprint and a scoped read that folds every entry
of the scope produce byte-identical `Collect` output, at every path, at every commit. It is
the property phase 2 tests, and the differential over genScopeOps is what tests it.

WHAT DOMINANCE CANNOT BOUND, said plainly. Repeated statements that cover nothing: inserts
into a positional array, arrays with object elements, empty objects, comment and tag
operations. A scope that appends to an unkeyed array a thousand times pays a thousand
statements at that path, and that is the array-as-unit story the write budget already tells
(rebuild_plan.md decision 9). A `!logd-key` turns an array of objects into an object of
names, and names are fields, and fields cover. Where verse has such arrays, that is the
lever, and it is verse's.

## What this plan does not do, and what it learned from

  - No scope snapshot, of the view or of the layer; no composition primitive; no kept scope
    document. Each was tried or designed and each is the wrong object for the reasons above.
  - The overlay (73e2637, archive/scope_overlay_plan.md) failed because it was DIFFED from two
    documents: a diff cannot say the scope deleted a field baseline never had, a union of
    owned paths applied its operand rather than placing it, keyed absence had no vocabulary.
    Dominance touches none of that: nothing is synthesized, the entries are the scope's own.
  - 3b03fa8 (worktree issue-5hmq80f3, July, never merged) built THIS SHAPE -- the latest
    write per node, distinct entries read once, applied in commit order -- and measured it at
    64x on a scope rewriting the same leaves (60ms to 0.95ms at n=1000). Its dominance rule
    was "at one node all writes share a path and older ones are absolutely superseded",
    which phase 7's probing showed is false for a tagged or commented value, an array with
    object elements, and an empty object; and it could not survive compaction because child
    segments were not maintained through it then. Both objections are answered now: the
    cover rules are the correct supersession, and compaction re-derives every segment of
    every entry it moves (7 AS BUILT, compaction's working set). This plan is that commit's
    shape with phase 7's rules, and its number is the magnitude to expect.
  - 727faf5's kept scope document made a RUN of scoped writes flat and any interleaving with
    baseline linear. It was the right measurement and the wrong mechanism; 3b removed it.

## The prerequisite: the escape is not the replace (mgg9nvt6h12krn6dksn0)

The covers are a function of what the vocabulary's operations MEAN, and the classification
leans hardest on `!raw`: a bare `!raw` is a total cover because "the operand lands as it
is". mgg9nvt6 says that is the bundling defect -- `!raw` should only escape, and merge like
any patch of its shape with its tags carried as data, while `!insert.raw` is the spelling for
"the value is exactly this, and it is data". Once that lands a bare `!raw` object covers
nothing at its own path; its leaves become the statements.

Persisting covers in the index before that is decided would persist a classification a
later semantic change makes false, and a false cover is an unsound skip in a scoped read,
which is data loss. So mgg9nvt6 is built FIRST, and it is the one phase in this plan that
changes what a stored byte means; everything after it is index and read-path work the log
can be rebuilt into. It is also the only phase that must not be done twice.

Three things it settles that the footprint needs:

  - `!insert` is a total cover, and `statement()` says today that it "says something
    relative to its neighbours". `insertOp.Patch` applies its child against ABSENCE and
    answers with the value whatever it is applied to: the definition of total. So
    `!insert.raw`, which `libdiff.escaped` already emits for every diffed value carrying an
    operation, is the shape dominance currently ignores. Conservative, so safe, and wrong.
  - `claimValue` (lower.go) emits `!insert.raw`. A claim then IS a total cover by the
    operation's own definition, which is the strongest form of the argument 3e527ec made for
    storing the claim rather than the difference: a difference is a merge object, and a merge
    object covers nothing at the object.
  - The readings agree. The index already descends into a `!raw` operand (`OperandPaths`
    answers the child at the same path); `patches.Roots`, `ClaimPaths` and `LowerSites` all
    stop at the `!raw` node. Under merge-as-data every one of them descends, and the one
    place they all still stop is `!insert`, which each already treats as an operation.

THE MIGRATION IS VERSE'S, and it is small. verse's `entity.Raw` and `AsData` both rely on the
replacing half explicitly (entity/raw.go: a charter's spec must replace so a merge does not
leave the previous version's fields standing; a rewritten goal must not accumulate its tags).
Both compose `!insert.raw` instead, one line in `rawTag`, and verse re-puts its escaped
records once. No delete is needed: `!insert.raw` replaces regardless of the old raw entries
still in the log, and a plain re-put avoids the window in which a trigger watching the
charter would see it absent. verse can ship FIRST, since `!insert.raw` already replaces on
today's go-tony, so the flip lands on a store that no longer writes a bare `!raw`. Old scope
claims are bare `!raw` from `claimValue` and would let baseline show through under a claim;
sandboxes are ephemeral, so live scopes are deleted at the verse upgrade with the
`DeleteScope` verse already runs when a sandbox goes. There is no vintage in `meta` and no
wipe.

THE MATCH SIDE stays exact, and the docs say so rather than leaving it as the thing that used
to justify the patch side. verse's `AsData` builds compare-and-swap preconditions from what
it read and needs literal comparison there.

## The order, and why it is this one

    P  the escape is not the replace   go-tony's half of mgg9nvt6; the vocabulary the covers
                                       are computed over is settled
    0  harness                         the interleave test, the counters, the differential
    1  the index knows the cover       the footprint, durable; DeleteScope and compaction's
                                       pass move onto it
    2  the scoped read seeks to it     the scope term of openRead; writes and preconditions
                                       inherit it; the numbers
    3  the scoped watch steps          own delta always; disjoint baseline delta; drop under
                                       a total cover; re-read otherwise

  - 1 needs P: a persisted cover is a claim about the log that must stay true.
  - 2 needs 1 and nothing else: the read asks the footprint and folds what it always folded.
  - 3 needs 2 for its re-read case to be bounded, and 1 for the three-way decision.
  - 0 needs nothing and goes first so the linear rows are printed by a test before anything
    changes them.

Release points: after P (with verse's `!insert.raw` already released ahead of it); after 2,
which is the first release the scoped rows can be measured against on staging; after 3.

## Ground rules

Those of rebuild_plan.md, unchanged: every commit compiles and its package's tests pass;
targeted `-run` in the edit loop and `go test ./system/logd/...` plus `gofmt -l` before a
commit; never GOTEST_LONG; the signature rule is law; commit messages in the repository's
register, prose, the reason before the mechanism; a deviation from this document is a commit
ON the document, never a silent divergence in the code. Below the line -- dlog, snap, patches,
the format library -- is not touched, with the named exceptions in phase P
(`mergeop/{raw,insert,patch,context}.go`, which are the change) and one in phase 1
(`patches.Roots`, which already exists for this purpose and gains nothing but a caller).

## Phase P -- the escape is not the replace

READ: the issue; mergeop/{raw.go,insert.go,operand_paths.go,context.go (OpContext),patch.go
(the object merge walk)};
libdiff/make.go (Escape, escaped); docs/matchpatch.md, the `!raw` section; api/lowering.go
(firstRelativeOp); api/storage_context.go (storableTags); storage/lower.go (claimValue);
storage/scope_compaction.go (statement, firstOperator); storage/raise.go:112;
storage/internal/patches/processor.go (Roots, hasOperation); storage/lower.go (ClaimPaths,
LowerSites).

BUILD:

  - `rawOp.Patch` merges: the same object walk with the dispatch off, a mode on `OpContext`
    as the issue describes (`RejectUnsafe` is the precedent), not a second traversal. A raw
    scalar lands as it is, tags as data. A raw object merges field by field into what is
    there, tags as data. A raw array merges as an array of the same shape does.
  - `rawOp.Match` unchanged: exact, tags compared. matchpatch.md says both halves, in the
    sentence that is missing today: a `!raw` PATCH merges as data, a `!raw` MATCH is exact.
  - `claimValue` composes `!insert.raw` over the claim. Nothing else in lowering changes.
  - `statement()`: `insert` is total; a bare `!raw` is classified by what it wraps, with
    every tag under it treated as data, which is the plain-value classification with the
    tag check off. `firstOperator` stays the reading of "the operation a node is applied by".
  - `Roots`, `ClaimPaths`, `LowerSites` descend into a bare `!raw` as the index already does,
    with tags as data; all four stop at `!insert`.
  - `libdiff.Escape` is checked for callers that wanted the replacing half; the overlay was
    the named one and is gone.

TESTS: the issue's three lines as a table test on `tony.Patch`, plus the same three under a
head comment. `Patch(a, Diff(a, b)) == b` for documents holding operations, unchanged. The
scope compaction differential over genScopeOps with claims in the new shape. A scope's
`!rename` still lowers to a claim that replaces (the defect claimValue's comment names). A
charter-shaped record put twice with `!insert.raw` reads as its second spec over a log whose
first put was a bare `!raw`. The match example in matchpatch.md, `rule: !raw {id: !glob
hot-*}` against a document with a `stage` field, still does not match.

DONE WHEN: `go test ./...` at the go-tony root is green, matchpatch.md states both halves, and
`statement()` classifies `!insert.raw` as total with a test that a claim dominates what it
covers. Release point, AFTER verse has released `!insert.raw` in `entity.Raw` and `AsData`
and re-put its escaped records.

## Phase 0 -- the harness

READ: scope_scaling_test.go; scope_watch_cost_test.go; scope_compaction_test.go (genScopeOps
and the differential); read_stats.go; the interleave test attached to sb33w8p9.

BUILD:

  - `scope_interleave_test.go`, from the issue's attachment, as `TestScaling_ScopedWriteInterleaved`
    beside `TestScaling_Writes`: a baseline commit between every scoped one.
  - The counters, named now and reported from phase 2: `reads.scope` (scoped reads),
    `reads.scope.footprint` (footprint nodes a read walked), `reads.scope.folded` (scope
    entries a read folded), `reads.scope.skipped` (live statements not folded because a
    cover above them in the same read made them unreachable -- expected zero, and reported
    because an unexpected non-zero is a defect in the maintenance), `reads.scope.wide`
    (scoped reads at the root), and the watch's `watch.scope.step`, `watch.scope.drop`,
    `watch.scope.reread` for phase 3. In `StatsReport`, beside `reads.narrow`.
  - The differential, generalised: `scopeDifferential(t, ops)` runs genScopeOps against two
    stores and compares `Collect` at every path at every commit in both views; phase 7 used
    it with compaction as the difference, phase 2 uses it with the footprint on and off.
    genScopeOps gains the shapes the cover rules name and one the fold's spine needs: a
    `!delete` below a spine only the dominated entry created.
  - A scope workload in `shapegen_test.go` (`shapedStore`): a sandbox writing K entities'
    leaves while baseline writes N others, with a watch at the shared ancestor. Phases 2 and
    3 measure against it.

DONE WHEN: the linear rows in "What is measured" are printed by `go test -run TestScaling`,
the interleave row with them, and every counter above exists and reads zero.

## Phase 1 -- the index knows the cover

READ: index_residency.md; index/{index.go (Add, Remove, DeleteScope, removeAll), region.go,
regions_file.go (Manifest, ManifestNode, Persist, Rewrite, OpenIndex), build.go,
log_segment.go (eachPatchSegment, passesThrough), patch_walk.go}; storage/scope_compaction.go;
storage/compaction.go (Compact, unindexEntry, reindexEntry); storage/index_persist.go.

BUILD:

  - `index/footprint.go` (new): per scope, a trie keyed the way the index's is; per node the
    live statements as (commit, log file, position, generation, need), in commit order. Two
    operations, both under the index's own locks: `state(scope, path, commit, ref, offers,
    needs)`, which appends and then drops what the statement's cover dominates -- at the node
    by need, and every statement under it -- and `forget(scope, ref)`, which is what a
    dropped or moved entry does to it. Answers, for the read: `live(scope, kp)`, the
    statements on kp's ancestor chain, at kp, and under kp, each entry once, in commit order;
    for the watch: `reaches(scope, kp)` and `totallyCovered(scope, kp)`; for DeleteScope:
    the paths.
  - ONE READING. The statement classification is `statement()` and the statement set is
    `patches.Roots`; both move to where the index can call them and are called from the
    same walk that derives an entry's segments, so `EachSegment` is the one derivation of
    both and compaction's unindex and reindex carry the footprint with the segments for
    free. `passesThrough` is aligned with `Roots` on the one shape they disagree about: a
    head comment wrapping a container is a statement at the comment's path (the fold keeps
    the earlier node's head comment under a later plain write, which is why it needs total),
    and is recorded as a write there rather than as spine. Decision 3 below.
  - DURABLE. The footprint is written in the manifest with every persist, `IndexFormatVersion`
    3 -> 4; a first open at 4 rebuilds from the log, as 3 did once. `Build` populates it in
    the same pass that indexes. It is resident, never paged, and reported: `index.footprint.
    scopes`, `index.footprint.statements`, `index.footprint.bytes`.
  - DeleteScope walks the footprint's paths and removes the scope's segments at those nodes
    and the footprint itself. It pages what those nodes need and nothing else.
  - Compaction's pass: an entry of a scope beyond the cutoff goes when it has no live
    statement. `dominatedScopeEntries` becomes a lookup by reference and reads no entry;
    the survivors' re-index moves their references in the footprint as it moves their
    segments. The replay floor rule is unchanged: a dropped scope entry raises it.

TESTS: `TestScopeCompactionDifferential` unchanged and green with the pass reading zero
entries (a counter, asserted). Footprint unit tests on the cover rules: each row of
`TestAScopesDominatedEntriesGoBeyondTheCutoff` restated as what is live after each write.
Reopen equals rebuild: a store closed and reopened has the footprint the log rebuilds. A
scope deleted pages no region outside its footprint (the residency's miss counter, asserted
against a scope that wrote under three of a thousand paths). A statement's reference follows
its entry through a compaction that moves it.

DONE WHEN: those pass, `index.footprint.*` is non-zero on the shapegen scope workload, and the
compaction pass's entry reads are zero. Release point is not here; nothing reads it yet.

## Phase 2 -- the scoped read seeks to the scope's cover

READ: cursor.go (openRead, readThroughAncestor, Read, provenAbsent); path_snapshot.go
(snapshotPath's nil scope, unchanged); commit_ops.go (stateAt); tx/match.go; read_stats.go.

BUILD:

  - The scope term of `openRead` is `footprint.live(scope, kp)`: the baseline loop is as it
    is, the scope loop reads the live statements' entries by reference, projects each to kp
    with the one projection, and folds them after baseline exactly as today. `Segments(kp,
    nil, &at, scope)` is no longer called by a read; it remains what `Deltas` walks, because
    a replay must see every entry.
  - `at` is honoured: a live statement above `at` is not folded. A read at a past commit
    behind a cover is therefore served by more entries than a read at the head, which is the
    right gradient and is what the compaction cutoff already implies for baseline.
  - Presence for a scope: a read at a path baseline proves unwritten is Absent when the
    footprint has nothing at, above or under it. `provenAbsent` takes the scope.
  - Writes and preconditions change nothing: `stateAt` is a `Read`.
  - Counted, per read, as phase 0 named.

TESTS: the differential with the footprint on against a store with it off, over genScopeOps,
both views, every path, every commit -- the invariant. `TestScaling_Reads` and `_Writes` and
the interleave test: every scoped row flat across 50 to 400. A scoped read at a path with a
claim above it reads through the ancestor as today. A scoped read at a never-written path
under a scope that wrote elsewhere opens no log file. At shapegen scale: a scoped read at
an entity the sandbox rewrote a thousand times folds one scope entry.

DONE WHEN: the invariant holds at 200 seeds, the four scoped rows are within 2x of their
baseline rows at N=400, and `reads.scope.folded` per read on the shapegen workload is
bounded by the sandbox's leaves under the path. Release point: the first the scoped rows can
be measured against on staging.

## Phase 3 -- the scoped watch steps

READ: server/session_watch.go (watchStream, stepBaseline, emitScoped, replay, the live loop);
server/session_read.go (scopedDocAt, readValueAt); api/delta.go (ProjectDelta); tick.go
(deliverable); scope_watch_cost_test.go.

BUILD: three cases, decided from the footprint with no entry read, in `emitScoped`:

  - THE SCOPE'S OWN COMMIT STEPS. Its delta applies last in the fold anyway, so applying it
    to the held value is the new scoped view; it is `stepBaseline`'s own code path with the
    scope's entry, and the delta sent is the projection, as a baseline watch sends.
  - A BASELINE COMMIT STEPS when its projected roots meet no scope statement at, above or
    under them (`reaches` is false for every root). Nothing of the scope's is displaced.
  - A BASELINE COMMIT IS DROPPED when a total cover of the scope stands at or above every
    root (`totallyCovered`). The scope's view there cannot change.
  - ANYTHING ELSE RE-READS at the path and diffs, which is today's behaviour, now bounded by
    phase 2. The replay path uses the same three cases per commit it replays.

The held value stays what it is today: the watched path's own value, and nothing wider.

TESTS: a scoped watch on a path the scope owns receives nothing for baseline writes under it
and one event per scope write; a scoped watch on a path the scope has not touched steps
baseline deltas with `watch.scope.reread` at zero; the overlap case re-reads and delivers
the same bytes the recompute delivered before (a differential over the three cases against
the recompute-and-diff watcher, on genScopeOps). `TestScopedWatchReadCostPerEvent` extended
to measure the diff as well as the read, at N=50..400 and at shapegen scale, for a watch at
the shared ancestor: per-event cost flat in the scope's history AND independent of the
document's size for disjoint baseline commits.

DONE WHEN: those pass, the extended watch-cost table is flat on both axes, and verse's scoped
root watch is measured on the shapegen store before and after. Release point.

Then the issues, from results: sb33w8p9 and 9b2vpggx against the tables; 4wpqh7t2 as
superseded, with the phase P argument recorded on it; mgg9nvt6 against phase P.

## Decisions the plan leaves open

Each has a recommendation, and each is raised before it is built.

  1. WHERE THE FOOTPRINT LIVES. In the manifest, which is one file and one `MaxCommit` to
     catch up from; or as a per-region header the way snapshot commits are, which would
     page with the region. Recommend the manifest: the footprint is consulted on every
     scoped read and must be resident, and a scope's live statements are not a property of
     any one commit range.

  2. PER-STATEMENT NEED, OR A BIT PER NODE. The footprint as designed records each live
     statement's need, so a whole cover drops the plain statements at its node and keeps a
     tagged one. The cheaper alternative is one bit per node, whether anything needing total
     has been stated since the last total cover, which is correct and merely unbounded for
     that rare shape. Recommend per statement: the footprint holds the statement anyway, and
     the need is one byte of it.

  3. ONE WALK OR TWO. Statements derived in `eachPatchSegment` as it derives segments, which
     makes `EachSegment` the one derivation and carries the footprint through compaction for
     free, at the cost of aligning `passesThrough` with `Roots` on the commented container;
     or a second walk with `Roots` per entry in `IndexPatch` and its own hooks in
     compaction's unindex and reindex. Recommend one walk. The alignment is a correctness
     improvement in its own right: a read at a sibling below a commented container pays one
     projection that says nothing, and a comment the fold would keep is no longer invisible
     to the index.

  4. THE WALK UNDER THE PATH. Phase 3 of the rebuild insisted nothing below a read's path is
     visited, because the shared trie under a shallow path is the store. `live(scope, kp)`
     walks the scope's own trie under kp, sized by the scope's footprint there, and never
     touches the shared trie's children. Recommend it as within the rule's intent, and this
     is the one place the design bends the letter; `reads.scope.footprint` is what says
     whether it ever costs.

  5. `at` IN THE PAST. A live statement is live at the head; a read at an earlier commit may
     need a statement a later cover dominated. Recommend: the footprint answers the head
     only, and a read at `at` below the scope's newest cover on the path folds from
     `Segments` as today, counted as `reads.scope.historic`. verse reads scopes at the head.

## The test corpus, as a ledger

Continued from test_corpus.md in the same form; filled in as each phase lands.

    TRANSFER   scope_cow, scope_head_kept, scope_relativeop, scope_premise, scope_indexloss,
               scope_scaling, scope_watch_cost, scope_compaction, raw_escape, lower_scope:
               they assert answers, and the answers do not change
    REWRITE    scope_compaction's `scopeEntries` may count the footprint instead of the
               index if the index stops being the cheaper question; the assertion stays
    ADDED      P: the !raw table, the claim-dominates test, the charter re-put
               0: scope_interleave, the differential harness, the shapegen scope workload
               1: footprint_test (covers as live statements; reopen equals rebuild; delete
                  pages nothing outside; references follow a moved entry; the pass reads
                  zero entries)
               2: the footprint invariant at 200 seeds; the flat rows; presence for a scope
               3: the three-case watch differential; the extended watch-cost table

## Consumers

    verse    `entity.Raw` and `AsData` compose `!insert.raw` and the escaped records are
             re-put, ahead of phase P's release. Live scopes are deleted at that upgrade.
             Scoped entity writes are plain objects whose roots are leaves and cover well;
             an array of objects in an entity is the one shape that does not, and
             `!logd-key` is verse's lever there. The scoped child store's watch at `verse`
             is the consumer phase 3 is for.
    docd     unchanged; a scoped session's reads and watches get faster underneath it.
    libctl   unchanged.
