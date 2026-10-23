# logd: index.regions grows without bound while the store stays open (6.4 GB beside 1.3 GB of logs)

On verse's staging (go-tony v0.0.244, `o sys up`, compaction cutoff 1d, upstream's snapshot policy), docd-0's data directory after 25h of uptime:

```
index.regions   6,402,693,150   2026-10-23 10:27:37 UTC
index.manifest     14,781,464   2026-10-23 10:27:37
logA              643,660,743   2026-10-23 10:29:00
logB              645,289,242   2026-10-23 10:52:07
```

The index is five times both logs together. Compaction runs back to back, ~25 min a cycle (`compacting original=66710 surviving=62814`, then `compaction complete removed=…` ~24 min later), at about one commit a second.

What the code says (v0.0.244):

- `index/regions_file.go`: index.regions is APPEND-ONLY, "a record no manifest refers to is garbage, and the file is rewritten without its garbage when the store closes".
- `Index.Rewrite` has one caller, `Storage.Close` (storage.go:544).
- Compaction re-indexes every survivor where the rewrite put it (compaction.go:136, `reindexEntry`), and the persister then appends regions for them.
- `o logd` and `o system compose` never call `Storage.Close` (yg5vx3z2h12krdj2vdn0): the one rewrite there is runs only in tests. On a deployment the file is never rewritten, not even across a restart.

So a store that compacts often keeps appending a fresh index of its survivors every cycle and drops none of the old ones. 6.4 GB over ~60 cycles is ~100 MB a cycle, which is about the size of an index of ~60k survivors. That fits, but it has not been measured: a second sample of the file's size after the next cycle would show the growth per cycle. Neither has been done on staging.

What it costs: disk alone so far. The claim is 30Gi and 29% used, growing ~6 GB a day while docd runs, so it fills in ~3 days from here. An earlier 10Gi claim on the same deployment filled in eleven days and docd could not start. Whether a large regions file also slows the reads that page regions in is not known.

The likely shape of a fix is reclaiming garbage while the store runs: rewriting the regions file when its garbage passes some fraction (after a compaction's re-index is the natural point, since that is what makes most of it), or keeping compaction's re-index from appending regions the manifest is about to supersede. yg5vx3z2 makes the close-time rewrite happen on a normal shutdown, which bounds the file by one run's growth and is not a fix for a store that runs for weeks.
