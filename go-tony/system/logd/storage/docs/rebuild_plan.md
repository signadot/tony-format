# Rebuilding logd above the line: the plan

The implementation plan for wk5w1ddkh12krj1tkxn0, written for one agent with a large context
to execute end to end. The five design documents say WHAT; this says in what ORDER, in which
FILES, held by which TESTS, and where the documents leave a decision that has to be raised
before it is built. It is not a sixth design: where it seems to decide something the
documents did not, that is a recommendation, and the list of them is at the end.

Read first, in this order, before any code:

    the issue          git issue show wk5w1ddkh12krj1tkxn0     (description and all comments)
    element_identity.md            prerequisite 3   a keyed array is an object of names
    read_write_interface.md        prerequisite 1   Read / Deltas / Begin, and no wide read
    presence.md                    prerequisite 2   nil is absent, null is null, once
    one_delta_shape.md             prerequisite 4   the stored absolute delta is the only one
    index_residency.md             prerequisite 5   the index is bounded on what it holds
    storage/read.go                the nine reads, in the words of the code being replaced

Do not load the whole tree. Each phase below names what it reads; the rest of logd is
23,817 lines of code and 28,314 of tests, and most of it is either being deleted or must not
be touched.

## The line, in files

BELOW THE LINE, carried literally -- not rebuilt, not reformatted, not "improved in passing":

    storage/internal/dlog          2,191    A/B switching, crash recovery, generations
    storage/internal/snap          1,543    traversal events + the {Path, Offset} path index
    storage/internal/patches       1,374    the streaming processor
    storage/internal/seq             156
    ir, stream, mergeop, libdiff, parse, encode, token, kpath -- the format library.
    229 non-test files and 120 released versions reach it. Nothing here needs it changed,
    and a change to it is a sign the plan has been left.

ABOVE THE LINE, rebuilt:

    storage/*.go                   4,724    22 files; 59 test files, 10,664 lines
    storage/index                  2,715    11 files; 18 test files
    storage/tx                     2,055    12 files;  8 test files
    server                         3,718    14 files; 21 test files
    api                            4,924    of which api_gen.go is 3,266 generated lines

`*_gen.go` and `schema_gen.tony` are generated from `//tony:schemagen` markers on Go types by
`make generate` at the go-tony root; edit the types, regenerate, and run `make
generate-verify` before every commit that touches a marked type.

CONSUMERS, which is the blast radius of every signature change:

    system/libctl        the controller client library, in this repo -- 36 imports of logd/api
    system/docd          the multiplexing server verse deploys -- storage.Open,
                         DefaultCompactionConfig, and server.{Spec,Mounts,StartTCP,...}
    cmd/o                2 files
    verse                a separate repo pinned to a RELEASE (v0.0.203, no replace directive);
                         4 files import logd/api. It declares no !logd-key or !logd-auto-id
                         arrays, and its staging store may be wiped.

That last line is an assumption this plan is allowed to make and states so it can be revoked:
there is no live data in the keyed-array form, so element identity needs no data migration,
and every persisted-format bump below (index, log entry shape) is free of compatibility
work. If a second consumer appears before phase 5 ships, this assumption is the first thing
to revisit.

## Ground rules for every commit

  - It compiles and its package's tests pass. In the edit loop, `go test ./system/logd/
    storage -run TestX`; before each commit, `go test ./system/logd/...` and `gofmt -l`.
    Never set GOTEST_LONG: four tests (scope_scaling, scope_watch_cost, read_subtree_scope,
    baseline_since_snapshot) are gated behind it and cost over a minute; they run once, by a
    person, before a release.
  - THE SIGNATURE RULE, from read_write_interface.md, is law from phase 3 on and is enforced
    by a test rather than by review: no exported function in storage or storage/index returns
    a slice, map, or node whose length grows with the number of commits, segments, or
    elements in range.
  - NO STRANGLER. New and old entry points may coexist on the branch during phase 3 and
    nowhere else; phase 3 merges as one unit, with the nine reads deleted, or it does not
    merge. Nothing in the tree ever calls both.
  - Commit messages in the repository's register: prose, the reason before the mechanism,
    keyed to issue ids, no bullet changelogs. Every commit is linked with `git issue link
    wk5w1ddkh12krj1tkxn0 <sha>`. A deviation from a design document is a commit ON the
    document plus a comment on the issue, never a silent divergence in the code.
  - The comment record is the most valuable thing in the tree. A rebuilt file carries the
    WHY of each non-obvious choice forward, keyed to the issue that earned it; a rebuilt file
    that drops the reason re-earns the bug.
  - Progress is narrated on the issue at each phase boundary: what landed, what the counters
    say, what was raised.

## The order, and why it is this one

    0  harness            tests onto a helper; the corpus classified; counters named
    1  presence, Go side  one function; closes the null-then-deleted watch bug
    2  element identity   node cost first, then names, then the stored form
    3  read/write         the cutover: Read/Deltas/Begin in, the nine reads and the head out;
                          per-path lowering lands INSIDE this phase
    4  one delta shape    notification from the stored entry; the marker goes
    5  the wire           presence on the wire, one rooting, protocol 2; verse in lockstep
    6  index residency    the inversion, the seam, LRU
    7  snapshots into     per-path snapshots under compaction; the 94.9x
       compaction

The dependencies that fix it:

  - 3 needs 2: a cursor seeks to a NAME, and until a keyed element has one the cursor cannot
    stop at it (element_identity.md, thqtmm2th12kr051jhn0).
  - 3 needs the per-path half of 4: with no stepped head, `verifyApplies` has no whole
    document to diff against, so lowering must be per path before the head can go. That
    half moves into phase 3; the rest of 4 follows immediately.
  - the per-path half of 4 needs 2: it is sound only because an op's effect stays under the
    node it tags, and identity is what removes `!key` from `patchMayAffect`'s exceptions.
  - 6 is made urgent by 2 (a node is not a map entry; names multiply nodes), which is why
    the node-cost fix is the FIRST step of 2 and not part of 6.
  - 7 needs 3: a per-path snapshot is a cursor at that path piped into a snapshot builder,
    and the seek that picks the nearest snapshot at-or-above is phase 3's.
  - 1 needs nothing and is one afternoon; it goes first so the branch has a passing
    behavioural fix on it before the long phases.

Release points, because verse takes releases: after 2 (no wire change, no keyed data); after
3+4 (no wire change -- this is the first release the staging bar can be measured against);
after 5 (protocol 2, coordinated with verse's four files and libctl); after 6; after 7.

## Phase 0 -- the harness

READ: storage/read.go, the test file list, read_stats.go, write_stats.go.

BUILD:

  - `storage/readat_test.go`: `readAt(t, s, kp, commit, scope) *ir.Node` and
    `readRootedAt(...)`, implemented today over `ReadStateAt` + the trim callers do, and
    over `Collect(Read(...), testBudget)` from phase 3. Move every black-box test onto them
    now -- 38 test files across the repo call the reads by name -- so the phase 3 diff to
    tests is one file. `testBudget` is a named constant: the rule that every call site
    states a number holds in tests too.
  - The corpus, classified. A first pass by identifier finds these storage tests reaching
    for internals that will not exist (steppedStateAt, the head, lowerWrite, the marker,
    LookupRange, ReadPatchesInRange, and so on):

        arraywrite baseline_since_snapshot comment_walk compaction_subpath compaction
        diffarray_gate head key_routes keyed_lowering keyed lower_claim_diff lower_claim
        lower_comment lower_matrix lower_scope lower patch_root_marker read_equivalence
        read_patches_stream replay_floor scope_head scope_indexloss scope_premise scope
        snap_read snapshot tick zz_diag                                    (28 of 59)

    and in server: comment_delta, patchmayaffect, path_error (3 of 21). The list is
    over-inclusive (a test that only INSPECTS `s.index` is black-box in spirit); the pass
    here settles each file as TRANSFER (moves onto the helper unchanged), REWRITE (the
    behaviour stays, the mechanism it pokes goes), or DROP (it tests the mechanism), and
    records the verdict in a comment at the top of each file. The remaining 31 + 18 are the
    behavioural specification and are not edited except for the helper.
  - The counters named. `read_stats.go` grows the three terms the bound is stated in --
    bytes emitted, largest record buffered, whether the seek found a snapshot at or above
    the path -- and the keyed-read counters from c3e53a2 are folded into them. Named now,
    reported from phase 3.
  - `storage/internal/shapegen` (test-only): builds a store shaped like the staging
    forensics at a chosen scale -- N paths under a few shallow ancestors, ~100 patches per
    path, two commits in three touching the root and `verse`, root-only snapshots every
    ~2,000 commits, no compaction. Default scale runs in seconds; a larger scale sits behind
    GOTEST_LONG. Phases 3, 6 and 7 measure against it.
  - The doc citation fixed: one_delta_shape.md names `scopeHasKeyedPaths` and
    `readScopedStateAtOverlay`, which do not exist. The keyed fallback it means is
    `storage/scope_keyed.go` -- `patchHasUndeclaredKey`, `keyedArrayPaths`, `annotateKeyed`
    -- reached from lower.go:98-99 and lower.go:234-235. The argument stands; the names
    do not.

DONE WHEN: `go test ./system/logd/...` is green with every read in tests going through the
helper, and each of the 80 test files carries its verdict.

## Phase 1 -- presence, the Go side

READ: presence.md; api/state.go; server/session.go (subtreeOf, 379); server/session_watch.go
(sendInitialState, stepBaseline); server/match_data.go (watchAbsence, 297).

BUILD:

  - The rule, once, in api/state.go beside `NextState` and `SameState`: a nil `*ir.Node` is
    ABSENT, null is `ir.Null()`, no layer maps one onto the other.
  - `subtreeOf` stops flattening: absent is nil; its three `ir.Null()` returns become one
    null and two nils. `stepBaseline`'s `SameState` gate then tells the truth it always
    could.
  - `sendInitialState` keeps sending null for absent -- the wire cannot say absent until
    phase 5 -- and `watchAbsence` keeps logging it. Nothing on the wire changes here.

TESTS: a watched path holding null that is then deleted delivers an event (the defect the
document exhibits); a watched path that is absent, then null, then absent again delivers
three.

DONE WHEN: those pass and `server` is green. One commit.

## Phase 2 -- element identity

READ: element_identity.md; storage/index/{node.go,tree.go,log_segment.go,index.go,
patch_walk.go,build.go}; storage/{schema.go,storage_schema.go,lower.go,scope_keyed.go};
storage/tx/{key_tags.go,merge.go,array_write.go}; ir/kpath.go and token's KPathQuoteField
(for the quoted field segment, which already exists in the grammar).

2a. A NODE COSTS WHAT IT HOLDS. `index/node.go` has `maxLeaf = 32` and a node allocates its
    32-slot leaf empty -- ~4 KB per node before a single segment. Allocate on first insert
    and grow toward 32. Measure before and after by loading the forensics `index.gob`
    (verse checkout, `scratchpad/docd-forensics/`, 170 MB, format version 1 -- so this
    measurement happens BEFORE 2e bumps the format) with `runtime.ReadMemStats` around
    `LoadIndex`. This is its own commit and its own number.

2b. NAMES, as a package with no store in it: `storage/ident` (new). A `Name` is bindings of
    identity fields to scalar values; `Canonical()` renders `(f=v)` for one and `<{...}>`
    for several, fields sorted, values through the inline encoder with tag and comment
    stripped; `Parse(segment)` returns a Name or reports WILD (a binding of a non-identity
    field, a partial composite, `*`); a null value is not a name. Pure functions, exhaustive
    unit tests, including that `(n=42)` and `(n='42')` are different names and that a value
    containing `=` is quoted. The three spellings resolve here: `items.'(sku=A)'` is the
    stored field, `items(sku=A)` and `items(A)` canonicalize to it at the boundary, the
    last against the schema.

2c. THE SCHEMA IS THE AUTHORITY. `tx/key_tags.go` ("one array has one identity") gains the
    clause: several `!logd-key` on one array are one composite identity; `!logd-key` with
    `!logd-auto-id` stays refused. A patch that carries `!key(f)` on an array the schema
    does not key is an ERROR. That replaces the fallback in `scope_keyed.go` entirely;
    delete the file.

2d. THE STORED FORM IS AN OBJECT. In `lower.go`, a keyed array's next state is
    `!logd-keyed(f[,g]) { '(f=v)': {...}, ... }`; `libdiff` then gives per-element deltas
    for free, because it is diffing objects. Raising is a projection at the boundary -- a
    stream transform in storage, NOT in `stream` -- that turns a `!logd-keyed` object's
    field events into `!key(...)`-array element events in field order, which is name order.
    The name is authoritative: an element whose key fields disagree with its name is refused
    at the write; a write that changes an identity field is refused at the write (it is a
    delete and an insert of a different element). The key stays in the element as well as
    in the name.

2e. THE INDEX KEYS ON THE NAME. `LogSegment` loses `ArrayKey` and `ArrayKeyField`;
    `IndexFormatVersion` 1 -> 2, and an older `index.gob` is rebuilt from the log by
    `index/build.go` as it is today on any mismatch. `patch_walk.go` walks the lowered
    object form, so its `!key` handling goes. The trie gains nothing: an element is a
    field, `Index.Children` already keys on segments.

2f. POSITION IS FOR UNKEYED ARRAYS ONLY. `tx/array_write.go`'s element-write checks apply
    to arrays the schema does not key, which remain values indexed AT the array;
    `tx/merge.go`'s `RootKeyedListAt` goes, and `RootPatchAt` builds `items.'(sku=A)'` as
    the ordinary field path it is (the third defect in thqtmm2th12kr051jhn0). `items[2]`
    on a keyed array is an error.

2g. GAINING AN IDENTITY. `StartMigration`/`CompleteMigration` (storage_schema.go): the
    migration commit writes, at each newly keyed array path, a `!replace` whose value is
    the object form. A read carries a commit and is on one side or the other. Losing an
    identity is refused. Built and tested; not exercised by any live store.

TESTS: transfer/rewrite schema_keyfield, keyed, keyed_lowering(_writes), key_routes,
key_duplicate, index_kpath, operand_paths. New: canonical names (2b); a keyed array
reordered by a client touches no index node; inserting one element into N writes one record
and one trie child, and reading one element reads O(1) records; immutability refusals; the
null-key decision (below); a read at a keyed element narrows, so `reads.wide.keyed-or-idx`
is 0 on a keyed workload.

DONE WHEN: the three defects in thqtmm2th12kr051jhn0 each have a passing test; the node
measurement is in a commit message. Release point.

## Phase 3 -- the read and write interface: the cutover

READ: read_write_interface.md; storage/{read.go,read_subtree.go,read_patches.go,absent.go,
head.go,scope_head.go,commit_ops.go,storage.go (replayBaselineAt 232, replayScopedAt 272,
patchNodesFromSegments 307, applyPatchesToBase 341),snap_storage.go,replay_floor.go,tick.go,
lower.go}; storage/index/{index.go (LookupRange 161, LookupSubtree 315),index_iterator.go};
storage/internal/snap/snap.go (ReadPathEventReader 123) and snap/index.go; storage/internal/
patches/processor.go (ApplyPatches); server/{session_read.go,session_watch.go,watch.go};
storage/tx/{coord.go,tx.go,match.go,array_write.go}; index/build.go.

3a. THE SEGMENT CURSOR. `index.Segments(kp, from, to, scope) SegmentCursor`, built from what
    `index_iterator.go` already has -- `IterAtPath`/`ToPath` down the trie and
    `CommitsAt(commit, dir)` at each node -- walking kp's ancestor chain and kp's own node,
    skipping `Spine` segments at ancestors. `LookupSubtree` and `LookupRange` are deleted,
    both slice-returning; every caller moves onto the cursor. (compaction.go's survivor
    selection holds slices over the whole index and is a compaction working-set question,
    not a read one -- see phase 7.)

3b. READ. `Read(at, view, kp) (Cursor, error)`, three steps and nothing held across them:
      seek     `baselineSnapshotSegment(at)` -> `snap.ReadPathEventReader(kp)` on that
               snapshot -- generalize `findSnapshotBaseReader` from the root to kp; the
               snapshot's own path index bounds it.
      compose  for each segment the cursor yields: read the entry from dlog, project the
               stored delta to kp (one projection function -- it is also phase 4's rooting
               rule), compose into the running delta with mergeop. One record and the
               composed delta resident. `patchNodesFromSegments` goes.
      fold     `patches.NewStreamingProcessor().ApplyPatches(base, [delta], sink)` -- one
               patch, bounded by the delta.
    `ApplyPatches` is PUSH-shaped (it drives events into a sink); `Cursor.Next()` is pull.
    The adapter is decision 2 below. `Presence()` answers Absent from the index alone where
    the spine proves the path was never written (absent.go's logic, no log opened), else by
    buffering the first event: none is Absent, a null scalar is Null. A cursor pins `at`
    and the log generation and holds no lock between `Next` calls (kds4sx3bh12krdrkghn0);
    compaction moving beneath it fails it retryably.

3c. `Rooted(c, kp)` and `Collect(c, budget)`. Collect refuses past its budget with a typed
    error; it is the only place a node is built from a cursor.

3d. DELTAS. `Deltas(from, view, kp) DeltaCursor` over `Segments(kp, from, head, scope)`,
    each stored entry projected to kp -- the ancestor of `EachPatchInRange`, which goes
    with `ReadPatchesInRange` (read_patches.go). The live side is the same projection
    applied to the entry the commit just stored; a watch is `Read` for where to start and
    `Deltas(at+1)` after.

3e. WRITE. `Begin(view) *Tx`, `Write(kp, delta)`, `Require(kp, want)`, `Commit()` as the
    storage facade over `tx` -- the multi-participant coordination in `tx/coord.go` stays,
    since the session protocol's NewTx uses it. `Require` is a bounded read at its own path
    plus a predicate: `tx/match.go`'s `evaluateMatches` takes a read function and gets
    `Collect(Read(head, view, kp), budget)` where its budget is a configured number;
    `commitOps.MatchStateAt` goes.

3f. LOWERING PER PATH (the half of prerequisite 4 that phase 3 cannot do without). For each
    path the patch writes -- its leaf-most op sites, not its root -- read the current value
    there (`Read` + `Collect` under a configured lowering budget; decision 5), apply the op,
    diff. `verifyApplies` stops taking whole documents. A write whose lowering exceeds the
    budget is refused as a write too large, which is the bound charging the client that
    asked for it.

3g. THE HEAD IS A NUMBER. `head.go` and `scope_head.go` are deleted whole -- stepHead,
    dropHead, installHead, CheckHead, steppedBaselineAt, steppedScopedAt. `CheckHead` is
    retired, not replaced. `createSnapshot` (snap_storage.go:164) becomes `Read(commit,
    baseline, "")` piped into `snap.NewBuilder`: the root read IS a cursor at the root, and
    the 511 MB of decoded patches the probe found there goes with the same function that
    removes it from reads.

3h. THE CALLERS. server/session_read.go: `readDocAt`/`fullDocAt`/`scopedDocAt` collapse to
    `Rooted(Read(...))` and the response is built under a server-configured budget
    (decision 3). server/session_watch.go: `sendInitialState` is a Read; `stepBaseline`
    and `replay` are one loop over `Deltas`; the watcher's kept `prev` document goes
    (rkb7p8v5h12ksdnmgsn0, taken all the way). tx/tx.go and tx/array_write.go read the
    unkeyed array's value at the array path. index/build.go and replay_floor.go move onto
    the index. absent.go's proof moves into `Presence()`.

3i. THE NINE, DELETED: ReadStateAt, ReadSubtreeAt, ReadSubtreeRootedAt, AbsentSpineAt,
    steppedStateAt, replayBaselineAt, steppedBaselineAt, replayScopedAt, steppedScopedAt,
    narrowSubtreeAt; with them applyPatchesToBase's slice form, `LowerEverything`, and
    read.go's table, replaced by a doc comment that says there is one read.

3j. THE RULE, ENFORCED. `storage/signature_rule_test.go`: parse storage and storage/index
    with go/parser and fail on any exported func or method whose result type spells
    `[]LogSegment`, `[]index.LogSegment`, `[]*ir.Node`, `[]*CommitNotification`, or a map
    of those. AST-level is enough -- the rule is about what a signature spells -- and it
    runs in milliseconds. An allowlist entry needs a reason in the test.

3k. THE BOUND, REPORTED. Every Read records bytes emitted, largest record buffered, and
    whether the seek found a snapshot at or above kp, into the counters named in phase 0
    and out through `StatsReport`.

TESTS: `readat_test.go` reimplemented over Collect; the 31 black-box storage files and 18
server files pass unchanged; the REWRITE set rewritten against behaviour. New, on the
shapegen store at default scale: a read of one field beneath a path carrying thousands of
patches and no snapshot reads O(writes to that path) records and reports a largest-record
term unrelated to the count; a read at the root completes and its counters match the
document size; one watch on one issue-shaped path while the generator commits at rate
delivers every delta and holds no document. A test that `Presence()` at a never-written
path opens no log file. A test that a cursor's `Next` blocks no concurrent commit.

DONE WHEN: the signature test passes with an empty allowlist; every counter is non-zero on
the shapegen store; no file in storage or server references a deleted name. MERGES WHOLE.
Release point (after phase 4).

AS BUILT, the phase split in two, and the split is worth recording because it is not the
strangler the failure list forbids. 3a landed the READ side whole: Read, Deltas, Collect and
Rooted in, the nine read entry points deleted, every caller moved, the signature rule as a
test. The stepped head stayed for 3b as an internal cache the commit path steps -- it is not
an entry point, nothing outside the store can reach it, and it is what per-path lowering
(3f) replaces. The allowlist is not empty: LookupRangeAll and AllSegments are whole-index
walks that compaction and persistence make by design, and phase 7 is where their working set
is decided. The one composition primitive the design named -- "composed into one running
delta" -- does not exist in the format library, and the processor folds several patches at
one path one by one onto the collected subtree; Read therefore holds the writes to kp since
the snapshot as a list, each cut down to kp, which is the "what changed under kp" term the
bound admits. A literal composition is a refinement, not a prerequisite.

3b AS BUILT: the head is a number. head.go and scope_head.go are gone; a write is verified
and lowered at each SITE it states something at -- the node an operation is written on, a
leaf, or the array a position reaches into -- by one bounded read of the value there under
the write budget (128 MiB, storage.writeBudget), the fold of the write's node onto it and,
where lowering is needed, the diff there; the site deltas are marked where they land and
rooted together by MergePatches, the construction a multi-participant write already has. A
scope is lowered at what it claims (ClaimPaths, through a comment to the leaf); baseline at
what it states (LowerSites, stopping at a commented node, since a diff taken below a comment
never sees it). A precondition is StateAt(path): one bounded read of the value it names, in
the store's form, against the lowered pattern. The refusal for a write past the budget names
the path, the operation and the budget, and reaches the client as its own mistake. The
marker now lands at the site rather than the container the whole-document diff descended
to; TestLoweredMarkerLandsOnTheChange records the one row where a scope differs, because an
absolute scope write is stored as sent under the client's marker.

## Phase 4 -- one delta shape, the rest

READ: one_delta_shape.md; storage/tick.go (newCommitNotification 199, DeliverablePatch 181);
storage/tx/patch_root.go; api/lowering.go (NeedsLowering 72); storage/lower.go (the scope
skip); the scope tests.

BUILD:

  - The notification is built from the STORED entry after lowering; `verifyApplies` wanting
    a stripped copy of what the client sent stays a local.
  - `NeedsLowering` may skip a diff it can prove is the identity; it decides nothing about
    shape. `LowerEverything` is already gone (3i).
  - `!logd-patch-root` deleted: tx/patch_root.go, `DeliverablePatch`, and
    patch_root_marker_test.go. Which subtrees a commit touched is answered by its segments.
  - One rooting rule: the projection function from 3b is the only one; Read, Deltas and
    the notification use it (rg5nd1psh12kse7dddn0).
  - Scopes: `lowerWrite`'s scope skip becomes one `storableDelta` for the change and the
    owned-path union as the overlay's own step, in the order 4wpqh7t2h12ks1fvj5n0 gives.

TESTS: IDENTITY -- for any commit, the delta a live watcher receives and the delta a
replaying watcher receives are the same bytes; ABSOLUTENESS -- applied at C-1 the delta
gives C, asserted at the commit; a commit that changed nothing notifies with an empty delta;
the ten scope_* files transfer as behaviour.

DONE WHEN: xmxt2p85h12ksjp1gsn0's shape (`!delete` vs `!delete.logd-patch-root`) is
unrepresentable. Release point, with phase 3.

4 AS BUILT: the notification is built after lowering from the entry the log keeps, and is
a deep copy of it (`deliverable`, storage/tick.go); Deltas hands out the same copy of the
same entry; both are raised by the one function, so live and replay are the same bytes by
construction and delta_identity_test asserts IDENTITY and ABSOLUTENESS at the commit, with
lowering as shipped and forced. The marker is gone -- tx/patch_root.go, DeliverablePatch,
markDeltaRoots, the strip at every hop -- and where an entry is applied from is read from
its shape (patches.walkAndCollectPatchRoots): an operation is about the node it is on and
its operand is not descended into; a leaf, an array, or an empty container is a write at
its path; a commented node is a statement at the comment's path; a plain object with
fields is passed through. That is the reading the index (PatchChildren) and the lowering
(LowerSites) already make, so the three agree because they are one rule. NeedsLowering
survives as the optimisation it is -- whether an absolute write is diffed or kept as sent
-- and cannot change what a watcher receives; `lowerEverything` is the unexported test
knob. One consequence is recorded in the processor tests: a bare array in an entry is
applied as a unit at the array's path, which is what the fold (api.NextState) does with
it, and the tests that had rooted an element by tag now expect the fold's answer. Not in
this phase: the ONE ROOTING RULE, which is where a watch's events are defined and lands
with the wire in phase 5.

## Phase 5 -- the wire

READ: api/session.go (WatchEvent 374, MatchResult 289, ProtocolVersion 48); server/
session_watch.go; server/match_data.go; libctl's event handling; verse's four files.

BUILD:

  - Presence on the wire (decision 4): `WatchEvent` and `MatchResult` can say absent
    without a null standing in for it. `ProtocolVersion` 1 -> 2; regenerate. `watchAbsence`
    stays as observability and nothing reads the log to learn a state.
  - The response is encoded from the event stream, so the server holds no node for a read
    (the second half of decision 3).
  - libctl moves; verse's four files move; a go-tony release is tagged and verse's go.mod
    bumped in one coordinated step. The staging store is wiped on deploy.

TESTS: protocol_version_test for 2; an absent path watched, delivered, and read back as
absent by a client without consulting a log; a large read whose server-side allocation is
bounded by the encoder's buffer and not by the document.

DONE WHEN: verse staging runs protocol 2 and the forensics bar is measured on it: the read
of one field under a hot path, the one-issue watch under load, and the root read, all with no
GOMEMLIMIT set.

## Phase 6 -- index residency

READ: index_residency.md; storage/index/{persist.go,index.go,tree.go,node.go,build.go};
storage/index_persist.go; server/fileconfig.go (where the ceiling is configured).

BUILD:

  - THE INVERSION (decision 6). The resident trie becomes a cache of a durable, seekable
    index. The whole-tree gob in `persist.go` cannot be paged and goes. The durable form is
    itself derived from the log -- `build.go` rebuilds it when it is missing -- so the
    invariant is cheap to state: EVICTION CHANGES A COST, NEVER AN ANSWER, because a missed
    region is read back from the durable index, and a missing durable index is rebuilt from
    the log.
  - THE UNIT IS A REGION, (trie node, commit range). The trie skeleton is the floor
    (simplest; measured before argued). A node's commit tree is admitted by region.
  - THE SEAM, verbatim from the document: `Admit(r, bytes) error`, `Touch(r)`, `Evict()
    []Region` -- and `Evict` returning a slice is bounded by what is being evicted, not by
    history; say so in the allowlist. LRU is the one implementation. Every resident region
    passes through `Admit`; there is no other way to become resident. A ceiling below the
    floor is refused at `Open`.
  - Reported: resident bytes, ceiling, hit rate, evictions per interval, in `StatsReport`.

TESTS: the invariant as a property -- a random workload read under a ceiling equal to the
floor and read unbounded produce byte-identical `Collect` output, with evictions > 0; a
ceiling below the floor refuses Open; the shapegen store served under a ceiling one tenth of
its unbounded resident size, with the hit rate printed.

DONE WHEN: the staging-shaped store runs under a configured ceiling and says so. Release
point.

6 AS BUILT: the durable index is index.regions (append-only records, one per region) and
index.manifest (which records are current, with every region's header), and index.gob is
gone; the resident trie is a cache of it, the skeleton the floor, each node's segments in
regions of at most 64 contiguous in StartCommit. A read pages what its range needs and
answers in the same critical section that installs it; a write inserts in that section
too; only a durable, clean region is evicted, and the persister is what makes regions
evictable. LRU, a configured ceiling with a floor of eight regions, the counters in the
report. The property holds against the same index unbounded and reopened from its files,
and against an unbounded store at every path and commit. Found underneath: a B-tree
emptied to nothing refused every later insert (index_residency.md, As built).

## Phase 7 -- snapshots into compaction

READ: storage/{compaction.go,compaction_policy.go,snap_storage.go}; the compaction tests
(compaction, compaction_crash, compaction_policy, compaction_subpath).

BUILD:

  - A per-path snapshot is a `Start == End` segment at that path whose body is
    `Read(commit, view, p)` piped into `snap.NewBuilder`. The seek from 3b already prefers
    the nearest snapshot at or above kp, so nothing on the read side changes.
  - Policy (compaction_policy.go): a path whose tail since its last snapshot exceeds a
    threshold gets one; the tiers already there collapse history beneath it. Per (path,
    scope), the store tends toward one snapshot plus a tail -- the 4,519,816 -> 47,648
    measured as headroom.
  - Compaction's own working set: `selectSurvivors`/`indexCopies` hold O(segments). Bound
    it by walking the index with the cursor, or state in the file why compaction is allowed
    a working set the read path is not. Either is acceptable; silence is not.

TESTS: after compaction on the shapegen store, a read at a hot shallow path reads at most
the threshold in records; segment count is within a small factor of paths x scopes plus
tails; the crash tests transfer unchanged (the substrate they exercise is untouched).

DONE WHEN: the index the store describes is a small multiple of its live paths. Release
point.

7 AS BUILT, first half: a snapshot is OF a path. Its entry carries the path (Entry.SnapPath,
nil for the root), its segment sits at that path and nowhere else, and a read seeks to the
nearest snapshot at or above its path with the greatest commit (index.SnapshotAtOrAbove),
opened at the read's path within it -- the root snapshot the switch takes is the case
kp == "". WHEN one is taken is decided by the reads: a read that folded more than the
policy's tail of records at a path (64) and emitted no more than its byte budget (1 MiB)
schedules a snapshot of that path as of the commit it read, off the reader, one at a time,
serialized with the switch by s.snapMu and written to the inactive log at or after the root
snapshot there, so the file's commits stay ordered; a write's own read of its site is a
read, so a hot written path is snapshotted too and its verification read shortens with it.
A read that did not finish, an absent path, a commit before the root snapshot, and a
subtree over the budget each decline. Retention keeps a snapshot of a path within the
cutoff and drops it beyond, and it takes no slot in the root snapshots' tiers. The
counters say what happened: reads.seek.path, reads.folded, reads.tail.max, snapshots.path.
The storage suite runs 20s faster under the default policy, which is the differentials'
own folds getting shorter.

COMPACTION'S WORKING SET (decision 8) is decided in the file, by construction: the work
list is the inactive log's own records, walked once (dlog.FileIterator); each entry's fate
is decided from the entry; a dropped entry's segments are derived from it and removed
before the rewrite, a survivor's re-derived and moved after it (index.EachSegment, the
walk that indexed it). What compaction holds is one record per entry of the file it
compacts and one entry at a time -- not every segment of both logs, which LookupRangeAll
and indexCopies held. This is also what made per-path snapshots correct: a work list taken
from the root's segments never saw a segment indexed only at its path, so a snapshot of a
path survived a rewrite at a stale position.

7 AS BUILT, second half (5hmq80f3h12krh1mbsn0): a scope's entry beyond the cutoff goes
when a later entry of the scope DOMINATES it -- states every path it stated, or an ancestor,
in a way that does not depend on what was there. That is sound because a stored scope
write is absolute (phase 3b), and it needs no patch composition: nothing is materialized,
the dominated entries are simply not there, and the fold of what remains is the fold of
everything. What a later statement covers is decided by what the merge keeps
(scope_compaction.go): !raw and !delete cover totally; a plain scalar or array of plain
scalars replaces its path and everything beneath, but at its own path may keep a comment
or a tag, so it dominates there only a statement that carried neither; an array with
object elements, an empty object, and every other operation cover nothing. The pass is one
walk of the scope newest-first holding the covers seen (patches.Roots is the reading), one
read per entry of the scope per compaction; after the first, the scope is its layer and a
tail. A dropped scope entry raises the store-wide replay floor like a baseline one, which
is the pessimistic side. TestScopeCompactionDifferential holds both views at every path
against a store that never compacts, and the scoped replay from above the floor.

## Decisions the documents leave open

Each has a recommendation. Each is raised on the issue as a comment BEFORE it is built, and
the answer (or silence for a day) is recorded in the commit that builds it.

  1. A NULL KEY VALUE in the object form. element_identity.md says such an element "is
     unkeyed and cannot be addressed", which is the merge's reading of a document; in the
     stored object form there is no field for it to be. Recommend: refused at the write, the
     same place immutability is enforced. Phase 2d.

  2. PULL OVER PUSH. `ApplyPatches` drives a sink; `Cursor.Next` pulls. Recommend: a
     goroutine per open cursor feeding a small bounded channel, `Close` cancelling it -- the
     bound holds (capacity x event size), and it needs nothing changed below the line. If a
     goroutine per cursor shows in a profile, a pull-mode processor in `patches` is the
     follow-up, and it is a substrate change to be costed then, not now. Phase 3b.

  3. THE SERVER'S RESPONSE. A match result is a node on the wire today. Recommend: phase 3
     builds it with `Collect` under a server-configured budget -- a number in fileconfig a
     reviewer can read -- and phase 5 encodes from the event stream so no node is held.
     Two steps rather than one because the second changes the encoder and belongs with the
     protocol bump.

  4. PRESENCE ON THE WIRE, field or event kind. Recommend: a field, `absent` with omitzero,
     on WatchEvent and MatchResult. Additive; a distinct kind adds a case to every client
     switch. Phase 5.

  5. THE LOWERING BUDGET. Per-path lowering reads the current value at each written path
     and that read has a size. Recommend: one configured `maxWriteBytes`, refused past with
     an error naming the path and the size. It is the "intermediate applied state" term the
     issue's bound admits, charged to the write that needs it. Phase 3f.

  6. THE DURABLE INDEX FORM. index_residency.md leaves "the current persisted form paged, or
     a form designed to be paged" open. The gob is one blob and cannot be paged. Recommend:
     an append-only regions file with a resident directory of (node, commit range) ->
     offset, O(regions) resident; rebuildable from the log like the gob is today. Phase 6.

  7. `Collect`'S BUDGET UNIT. Bytes, as read_write_interface.md writes it; events is a
     second knob with no second reason yet. Phase 3c.

  8. COMPACTION'S WORKING SET. See phase 7; a choice, made in the file.

  9. A PATCH EXPLODER, someday. An operation's site is the node it is written on, and !all
     over a container -- or a !replace of one -- asks for the whole container as the
     intermediate a write needs, which is what the write budget refuses past 128 MiB.
     The remedy the store could offer is to explode one such write into one write per
     element, each within the budget, committed as one transaction: the natural
     continuation of per-path lowering, and not an exception to it. Not built; noted at
     WriteBudgetError, where the refusal is.

## The test corpus, as a ledger

    storage    59 files   31 TRANSFER (helper only)    28 to classify REWRITE / DROP
    server     21 files   18 TRANSFER                   3 REWRITE
    tx          8 files   patch_root_test DROP (phase 4); match_test and merge_test
                          REWRITE (Require; RootKeyedListAt gone); the rest TRANSFER
    index      18 files   range/commit_range/index_within REWRITE onto the cursor;
                          segment_codec REWRITE for the new LogSegment; the rest TRANSFER
    dlog/snap/patches     untouched -- 20 files, 5,840 lines, they must pass without edits
                          at every commit, and an edit to one is a review flag

The rule of the ledger: a test moves from TRANSFER to REWRITE only with a sentence in its
file saying which mechanism it was holding onto; a test is DROPPED only if the behaviour it
asserted is asserted elsewhere by name, and the commit message says where.

## Measuring against the bar

The bar, from the issue: a store of the staging shape served with a working set set by the
delta and the path, with no heap ceiling configured. Three cases -- a field under a hot
unsnapshotted path, a watch on one issue under commit load, and the root read.

  - In tests: the shapegen store, default scale, with the counters from 3k asserted -- they
    are deterministic where `MemStats` is not. One coarse `HeapAlloc` assertion per case at
    the GOTEST_LONG scale, never in the loop.
  - On staging: after phase 5, with the store wiped and the sources allowed to run, the
    same three cases read from `StatsReport` and from the pod's memory series. The
    forensics tools (`tools/indexstat`, `tools/docsize`) are reused for the comparison.
  - The number that says the shape held: index resident bytes under the phase 6 ceiling,
    and largest-record-buffered on the root read equal to one record and not to the log.

## What would make this fail, restated as things to watch for in the diff

  - A new exported function returning a slice of segments or nodes "just for this caller".
    The test in 3j is what says no; do not allowlist it.
  - A file under storage/internal or in the format library appearing in a diff.
  - A comment that explains a mechanism and drops the issue id that earned it.
  - `ReadStateAt` surviving under another name because a test wanted a whole document.
    The test wants `Collect` with a number in it.
  - Phase 3 merged in parts.
