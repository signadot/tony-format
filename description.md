# logd: compaction maintains only root index segments, so per-path segments below the root keep stale positions and break watch replay

Severity: MEDIUM (opt-in: only bites when Config.Compaction is set). Confidence: measured + read from source.

Compaction removes and repositions index segments at the document ROOT only. Every log entry is also indexed at every path inside it, and those copies are left behind pointing at the pre-compaction LogPosition and LogFileGeneration. Any consumer that reaches an entry through a below-root segment then reads a position that no longer holds it.

## The mismatch

`Compact` sources its work list from the root (compaction.go:35):

    allSegments := s.index.LookupRangeAll("", nil, nil)

`LookupRangeAll`/`LookupRange` with kp == "" return that node's own commits and do not descend (index/index.go:163), so `inactiveSegments` never contains a segment indexed below the root. `updateIndexPositions` (compaction.go:266) and `removeFromIndex` (compaction.go:308) then Remove/Add those same segments, each carrying KindedPath "" — `index.Remove` navigates by KindedPath, so it too touches the root node only.

But `indexPatchRec` (index/log_segment.go:97) adds a segment for an entry at kPath "" AND at every path within the patch. So a compaction that drops or moves N entries leaves every below-root copy of those N entries in the index, unchanged.

## Measured

In-package test: 5 commits to `demo.x.hot`, SwitchDLog, 3 more commits, SwitchDLog, then `Compact` with Cutoff 0 (everything past the cutoff):

    root segments                     10 -> 7
    segments reachable from demo.x.hot 34 -> 31
    unreadable segments reachable from demo.x.hot: 9
      kp="demo"          commit=6  B@0    gen=0: read interrupted by compaction
      kp="demo.x"        commit=6  B@0    gen=0: read interrupted by compaction
      kp="demo.x.hot"    commit=6  B@0    gen=0: read interrupted by compaction
      ... same for commits 7 and 8

    ReadPatchesInRange("demo.x.hot", 0, cur, nil)
      -> failed to read entry at B:0: read interrupted by compaction

Three entries were compacted away; their root segments went, all nine below-root copies stayed.

## Blast radius

Consumers that still resolve entries through below-root segments:

- `ReadPatchesInRange` (read_patches.go:91) — watch replay / catch-up from a commit. Errors outright, as above.
- `ListRange` (index/index.go:334) — lists a child if it has any segments in range, so it can list paths whose entries were compacted away.
- `readPatchesAt` (read_patches.go:25) — testing/development helper.

State reads were affected too until 67220e2 (issue bvm163tyh12krwcqcsn0): `readBaselineStateAt` took its patch range at the read's path. It now takes it at the root, so `ReadStateAt` is clean after a compaction — verified in the same test.

Note also that `persistedIndexStale` (storage.go:258) scans `LookupRangeAll("", nil, nil)` — root only — so the startup guard for generation mismatch cannot see these segments either. A rebuild from the logs repairs them; loading a persisted index does not.

## Fix directions

1. Give compaction the whole segment set: an index walk that yields every node's segments with their full kpaths. Removal already works once you have them, since `index.Remove` navigates by KindedPath. Most direct, and keeps the below-root index authoritative.
2. Make below-root segments non-authoritative for entry lookup — a "which paths did this commit touch" structure only — and have `ReadPatchesInRange`/`ListRange` resolve entries through root segments, filtering by path. This is the direction the read path already took in 67220e2, and it makes the duplicate-per-level indexing cheaper rather than just correct.

A regression test wants to assert that after a compaction, every segment reachable from a below-root path still resolves to a readable entry — the current compaction tests only ever look at the root.

Files: system/logd/storage/compaction.go (Compact, updateIndexPositions, removeFromIndex), system/logd/storage/read_patches.go (ReadPatchesInRange, readPatchesAt), system/logd/storage/index/index.go (LookupRangeAll, LookupRange, ListRange), system/logd/storage/index/log_segment.go (indexPatchRec), system/logd/storage/storage.go (persistedIndexStale).

Found on go-tony v0.0.96, darwin/arm64.