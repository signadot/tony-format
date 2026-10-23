# logd: a narrow read walks every retained entry at the root and verse nodes (Spine segments ranged, then filtered)

On verse's staging (go-tony v0.0.244, docd-0 on a 2-vCPU node, compaction cutoff 1d, ~66k retained entries) a 30s CPU profile of the `o sys up` process (`/debug/pprof/profile`, 2026-10-23 11:37 UTC) puts 49% of all CPU in `Storage.Read`, and inside it the index walk: `Index.Segments → Tree.Range → node.parentRange / leafRange` (33.9% in `leafRange` alone), with `sort.Find`, `segmentsWithin`, `touched`, `missing` and `commitsUpTo` beside it. Decoding entries is ~3% (`token.*`), `DLog.ReadEntryAt` 4.5%, GC ~15%. The box runs both cores flat (load 2.45, 1.9 cores consumed over 25h, 97% user).

Why each read walks so much:

- `IndexPatch` records a segment at every prefix an entry passes through, as Spine where the patch merely passed (index.go, "TestEveryEntryIsIndexedAtEveryPrefix"). So the root node, and `verse` under it, each hold one segment per retained entry: ~66k each on this store.
- `Index.Segments(kp, from, to)` takes one list per node on the path to kp (`segmentsWithin`), and `segmentsWithin` ranges the node's `Commits` tree with `commitsUpTo(to)` as the comparator: by its own comment "a KEEP and not a prune ... What that costs is a cheap comparison per segment at or below `to`". `from` only chooses which regions are resident. The Spine filter (`!atKP && c.Spine → false`) and `inCommitRange` run per visited segment, after the visit.

So a narrow read at any path visits roughly every retained entry's header twice (root and `verse`) before it finds the few segments it keeps. At the store's ~40 reads/s that is ~21ms of CPU per read (25s of `Storage.Read` in a 30s window with ~1,200 reads), 0.85 of a core, and the comment's "cheap comparison" is the hottest function on the deployment.

What it costs downstream, measured on the same store:

- Compaction's own work is ~4 CPU-minutes a cycle (`reindexEntry` 4.66s in the 30s sample, 9% of CPU) and takes 25 wall-minutes, because the reads hold the rest; and since `maybeCompact` holds its flag until `SwitchDLog` returns, no snapshot fires meanwhile, so the replay window is ~1,500 commits instead of the policy's 1,000.
- Every commit's `apply` (`lowerWrite` reading its site back) has a ~300ms floor right after a snapshot, growing to ~570ms at the end of the window, 25–85% of commits over the 250ms `slowCommit` line depending on the hour. The floor is queueing behind the index walks, not the read itself.
- A watch's `sendInitialState` is the same walk (7.8s of the 30s), so watch (re)establishment pays it too.

The design intent in `Segments`'s comment is right ("bounded by the writes to that path in the range, not by history"); the Spine entries at the ancestors are what break it, since at an ancestor every entry is history. Shapes that would hold the intent: keep a node's Spine segments apart from its writes so an ancestor's walk sees only writes; or bound the walk below at `from` (the snapshot the read starts from) instead of filtering after the visit; or both.

Profile and the goroutine dump that led here are in verse's session notes; the numbers above are from `docd-0`'s log (`slow commit` lines carry `apply`/`append`/`index`) and the profile.