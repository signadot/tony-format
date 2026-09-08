# The test corpus, as a ledger

Phase 0 of rebuild_plan.md. Every logd test is in-package; this says, per file, whether it
asserts an ANSWER (TRANSFER: it moves onto the read helpers in readat_test.go and is not
otherwise edited) or holds onto a MECHANISM that the rebuild removes (REWRITE: the behaviour
stays and the test is restated against it; DROP: the behaviour is asserted elsewhere by name,
and the commit that drops it says where). The phase column is the phase whose diff touches the
file. A file moves from TRANSFER to REWRITE only with a sentence saying which mechanism it was
holding onto.

The classification is by identifier -- which internals a file names -- and is over-inclusive
in one direction: a file that only INSPECTS the index through a slice-returning lookup is
TRANSFER in spirit and REWRITE in mechanics, because the lookup's signature changes.

## storage (59 files)

    TRANSFER, 31 -- helper only, no other edit expected

        cas comment_payload compaction_crash compaction_policy corrupt_open dotted_path
        index_key_range index_version key_duplicate keyed_lowering_writes keyed_read_stats
        narrow_selectivity quoted_snapshot_read raw_escape read_snapshot_cases
        read_subtree_scope read_subtree replay_floor(reads) schema_authority
        schema_inject_key schema_keyfield schema scope_cow scope_head_kept scope_relativeop
        scope_scaling scope_watch_cost storage unsafe_write verify_write watermark

    TRANSFER with a signature edit -- reads the index through a slice

        key_routes     AllSegments                         3   iterator form
        lower          AllSegments                         3
        scope_premise  AllSegments                         3
        snapshot       LookupWithin                        3
        keyed_lowering storableDelta                       -   survives as is
        arraywrite     the marker, twice                   4   assertions on it go

    REWRITE

        baseline_since_snapshot  replayBaselineAt          3   cost-since-snapshot becomes the counters
        comment_walk             annotateKeyed,
                                 patchHasUndeclaredKey     2   the schema is the authority; the walk stays
        compaction_subpath       LookupRangeAll, ReadPatchesInRange
                                                           3   onto the segment cursor and Deltas
        compaction               LookupRangeAll            3
        diffarray_gate           lowerWrite                3   per-path lowering
        keyed                    ReadPatchesInRange,
                                 AllSegments, LookupRange  2,3 the object form, then the cursor
        lower_claim_diff         the marker                4
        lower_claim              lowerWrite, LowerEverything
                                                           3,4 every write lowers; the toggle goes
        lower_comment            replayBaselineAt, LowerEverything, the marker
                                                           3,4
        lower_matrix             LowerEverything x4        4   the plain/low pair collapses to one store
        lower_scope              LowerEverything x3        4
        read_equivalence         findSnapshotBaseReader    3   the seek is Read's
        read_patches_stream      EachPatchInRange x9       3   onto Deltas
        replay_floor             ReadPatchesInRange x5     3   onto Deltas
        scope_indexloss          LookupRange, ReadPatchesInRange
                                                           3
        scope                    ReadPatchesInRange x6     3   the reads transfer; the range walks move
        snap_read                findSnapshotBaseReader, replayScopedAt, replayBaselineAt
                                                           3
        tick                     ReadPatchesInRange, LookupRange
                                                           3,4 the notification is the stored entry
        zz_diag                  LookupRangeAll            3

    DROP

        head              the head is a number; read agreement is read_equivalence      3
        scope_head        the same, for a scope                                          3
        patch_root_marker the marker does not exist; one_delta_shape.md's identity test  4

## server (21 files)

    TRANSFER, 18

        absent_read duration_config empty_store fileconfig narrow_match protocol_version(5: bumps)
        relative_watch replay_compacted resume_point session snapshot_offpath snapshot_stress
        snapshot strict_config tcp unquote_field_key watch_ended watch

    REWRITE

        comment_delta    subtreeOf normalization                         1   absent is nil
        path_error       subtreeOf                                        1

    DROP

        patchmayaffect   the prefilter over a held document; the index says which paths
                         a commit touched                                 3

## tx (8 files)

        patch_root       DROP     4
        match            REWRITE  3   Require is a bounded read plus a predicate
        merge            REWRITE  2   RootKeyedListAt goes; RootPatchAt takes the field path
        tx               REWRITE  3   mockCommitOps implements the new CommitOps
        auto_id comment_walk raw_boundary unquote_field_key   TRANSFER

## index (18 files)

        range commit_range index_within range_bounds   REWRITE  3   onto the segment cursor
        segment_codec                                   REWRITE  2   LogSegment without ArrayKey
        index_kpath operand_paths                       REWRITE  2   names as segments
        build comment_index concurrent index_iterator index iterator node persist_lock
        schema tree_shape walk_lock                     TRANSFER

## below the line

    dlog (4 files), snap (9), patches (7), autoid (1): 21 files, untouched at every commit. An
    edit to one is a review flag.

## Phase 2, as it happened

Element identity moved the keyed tests further than the ledger above expected, because every
one of them wrote `!key` with no schema declared, which the design refuses.

    DROPPED   keyed_lowering, keyed_lowering_writes -- the annotate-and-tag lowering they
              specified does not exist; its behavioural content (a delta indexes the element
              it changed, a rebuild agrees with the live index) is in identity_test and keyed
    REWRITTEN onto a declared identity: baseline_expressivity (keyed cases), scope_cow
              (KeyDurability), index_key_range (what a name can spell; the rest refused),
              key_duplicate (two elements, one name: refused), key_routes (one route),
              keyed_read_stats (a keyed read narrows), keyed (declares; the two route-
              divergence tests go), lower (stored-as-sent, with the keyed list respelled),
              schema_authority / schema_keyfield (Identity; names as paths; two keys are one
              identity), comment_walk (storage: the two deleted helpers' subtest goes; tx:
              LowerKeyed through comments), raw_boundary, merge (RootKeyedListAt goes),
              index/schema_test (names as fields, positions as positions), api/schema_test
    ADDED     identity_test (storage), ident (package), keyed_path_test (server)
    TOUCHED   tick_test: a resolver that reads re-enters itself now that reads consult the
              schema; the probe guards its own re-entry

## Phase 3a, as it happened

The read side of the cutover: Read / Deltas / Collect / Rooted in, the nine read entry
points out, every caller moved, the head kept as an internal cache with no entry point
(3b removes it with per-path lowering). The behavioural corpus transferred through
readat_test.go with four assertions restated:

    REWRITTEN  read_subtree (FallsBackUnderAnOperator -> AnswersThroughAnOperator: the read
               answers through the ancestor the operator is on, and is counted WideOperator);
               keyed_read_stats (a rooted read at a position is refused before it is opened);
               server/absent_read (absence is classified from the nearest present ancestor's
               first event, so "verse.gone.zz" is no longer more generous than the document)
    RENAMED    ReadPatchesInRange / EachPatchInRange -> readPatchesInRange / eachPatchInRange
               (test helpers over Deltas); s.index.LookupRange -> segmentsAt (over Segments);
               index tests onto the unexported lookupRange; tx mocks onto ValueAt
    ADDED      index/cursor_test (the invariant Segments rests on), signature_rule_test
    FOUND      the patch-root marker made the root non-spine on every commit, so a read at
               any path collected every entry through the root's copy; passesThrough now
               looks through logd's own marker

## Phase 3b, as it happened

The write side: the head removed, per-site verification and lowering, preconditions as
bounded reads, the 128 MiB write budget.

    DROPPED    head, scope_head (the mechanism); tx/match_test (valueAtPath: a precondition
               reads the value at its path now, and TestPreconditionOnAKeyedElement reads a
               quoted segment); TestAClaimRefusesWhatItCannotRead (claimDelta navigated a
               whole next; the site's next is in hand)
    REWRITTEN  lower_comment, lower_matrix: "the head and a replay agree" became "a
               watcher's fold of the delta and the read agree", comments counted;
               scope_head_kept: the mechanism assertion off, the behaviour kept;
               patch_root_marker: the marker lands at the site, and the one row where a
               scope stores as sent is recorded
    ADDED      write_budget_test (a wide operation refused, the narrow one and the plain
               merge admitted, a precondition as a bounded read, a write reads only what it
               writes)
    FOUND      a write with a head comment on a container is a statement at that container:
               ClaimPaths walks through the comment to the leaf (right for a claim), and a
               baseline diff taken at the leaf lost the comment. LowerSites stops there.

## Phase 4, as it happened

The notification from the stored entry, and the marker gone.

    DROPPED    tx/patch_root, tx/patch_root_test, patch_root_marker_test (the marker and its
               tests); DeliverablePatch (a copy is all there is to do); markDeltaRoots;
               TestArrayElementInsertStoresNoMarker's assertion (nothing to store) -- it now
               checks the inserted element carries only what the client wrote
    REWRITTEN  patches: TestPatchRootsThroughComments -> TestCommentedNodeIsARoot (a comment
               above the first field of a block is the block's, so `# note` inside `a:`
               roots at a); the collector, index and processor tests that rooted an entry by
               tag root it by shape, and the positional-array expectations became the fold's
               ([null, 20, null] is what api.NextState makes of it);
               TestBuildPatchValueIndex_DominatedRootsFoldOncePerEntry dominates with an
               operation, since roots within one entry are disjoint by construction
    RENAMED    LowerEverything -> lowerEverything (a test knob, unexported);
               TestTick_NotificationPatchIsPrivateAndStripped -> ...IsPrivate
    ADDED      delta_identity_test (IDENTITY and ABSOLUTENESS at the commit, as shipped and
               with lowering forced; a lowered !insert, a comment as a wrapper, a write that
               changes nothing, a scope's claim, two deletes)
    FOUND      the processor tests rooted entries where no write produces a root -- an
               element inside a bare array -- and expected positional patching there; rooted
               by shape, the array is applied as a unit, which is what the fold does, so the
               tests' reading was the marker's and not the store's
