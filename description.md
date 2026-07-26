# logd: snapshots are indexed at the document root but looked up at the read's path, so no read below the root ever uses one (state reads stay O(entire history) forever)

Severity: HIGH (unbounded latency growth on every read, and on every read-modify-write). Confidence: measured + read from source.

Snapshots are created on schedule and are never used by any read below the document root, so `ReadStateAt` replays and re-parses the log **from commit 0** for the life of the document. There is no sawtooth: the cost never resets.

This is the mechanism behind the "O(history) re-parse per Match" noted in v67hjrjbh12ksarmcdn0, but it is a distinct and much smaller fix: the snapshot machinery works, the read path just cannot find what it produced.

## The mismatch

`createSnapshot` indexes the snapshot segment at the document root (snap_storage.go:203):

    snapSeg := &index.LogSegment{
        StartCommit: commit, EndCommit: commit,
        KindedPath:  "",              // <- root
        ...
    }
    s.index.Add(snapSeg)

`findSnapshotBaseReader` looks for that segment at the READ's path (snap_storage.go:23):

    iter := s.index.IterAtPath(kp)    // descends to kp's index node
    ...
    for seg := range iter.CommitsAt(commit, index.Down) { ... }

`IterAtPath` walks Children down to kp and never consults ancestors (index/index_iterator.go:33-46), so a segment on the root node is invisible from any descendant node. Every read at a non-root kpath falls into the `snapSeg == nil` branch, gets `patches.NewEmptyEventReader(), 0`, and `readBaselineStateAt` then does `LookupRange(kp, &0, &commit)` — the whole history.

Note `createSnapshot` itself calls `findSnapshotBaseReader("", commit)`, at the root, which is why snapshot CHAINING works while reads do not. Only reads at a real path miss.

## Measured

In-process logd (`logdserver.New(&Spec{Storage: storage.Open(tmpdir)})`, default config, so `Snapshot.MaxCommits = 1000`), one client session, sequential 1-field patches to a single path, `Match` latency sampled every 100 commits:

    commits   Match at verse.demo.x.hot   Match at verse
        100                     14.2 ms          5.2 ms
        200                     31.2 ms         11.0 ms
        400                     59.8 ms         21.3 ms
        700                    113.5 ms         38.9 ms
        900                    150.5 ms         50.8 ms
    -- "triggering snapshot" commitsSinceSnapshot=1000, "snapshot created" commit=1000 --
       1000                    172.1 ms         56.1 ms
       1200                    217.0 ms         71.3 ms
       1400                    253.3 ms         82.8 ms

The snapshot at commit 1000 fires, is written, and changes nothing: both curves continue straight through it. `verse` misses too, being a child of the document root — a read has to be at `""` to hit the snapshot, which no client does.

Writes are flat over the same range (~0.6 ms per `Patch`), so this is entirely the read path.

Cost is a function of the WHOLE log, not of the path read. After 700 commits to `demo:x:hot`, reading `other:y:untouched` — one commit, ever, at a sibling path — costs 40 ms. That is `LookupRange` over the root range plus `patchNodesFromSegments` re-parsing every entry.

## Why it bites downstream

verse's `LogdStore.Put` does a `Match` before its `Patch` (to null out fields dropped from the new value, giving Put whole-value replace semantics), and `Modify` does `Match` + `PatchIf`. So a WRITE costs a read, and a loop of N writes is O(N^2): 300 sequential entity writes take ~12 s, of which ~11.9 s is the reads. At a few thousand commits a single entity write is ~0.5 s. Nothing recovers as the log grows.

## Fix direction

Smallest correct fix: make the snapshot reachable from a descendant path — either walk ancestors in `findSnapshotBaseReader` (root node last), or index the snapshot segment on every path it covers. The former is cheap and local; `snapshot.ReadPathEventReader(kp)` already extracts the right subtree once a snapshot is found, so the base reader is correct as soon as the lookup finds it.

Worth a regression test that asserts read latency (or, better, the count of log entries parsed) DROPS after a snapshot — the current snapshot tests check that the snapshot is written and that state is correct, so a snapshot nothing reads still passes.

The parse cache in v67hjrjbh12ksarmcdn0 is still worth having for the residual replay between the last snapshot and current, but with this fixed that residual is bounded by `MaxCommits` instead of by the age of the document.

Files: system/logd/storage/snap_storage.go (findSnapshotBaseReader, createSnapshot), system/logd/storage/storage.go (readBaselineStateAt, readScopedStateAt, patchNodesFromSegments), system/logd/storage/index/index_iterator.go (IterAtPath).

Found on go-tony v0.0.95, darwin/arm64.