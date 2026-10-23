# logd: on verse staging, a few-byte commit spends 0.5-0.9s in apply while 25-minute snapshot+compactions run back to back

Observed on verse's staging store (docd-0, go-tony v0.0.244 as built into signadot/verse-staging:d58d1b739, an m6i.large: 2 vCPU, 8 GiB; memory request 3Gi, limit 5Gi; no CPU limit) on 2026-10-23. Users notice: every verse command waits on the store, `verse c` takes 43-49s, `verse status` 10s, a plain read 1.5-3s. versed itself is ~9% CPU over a 40s profile taken during one of those `verse c`; it is waiting on docd.

The store's config is only `compaction: {cutoff: 1d}`.

## Tiny commits spend 0.5-0.9s in apply

Over 10 minutes (07:00-07:10Z), 394 slow-commit lines; 352 of them at one path, summing to 254s of commit time, 42% of the wall clock:

    WARN slow commit path=verse.verse.iods.github.out took=833ms apply=820ms append=0s index=12ms other=0s commits=122461
    WARN slow commit path=verse.verse.iods.github.out took=881ms apply=864ms append=0s index=16ms other=0s commits=122462
    WARN slow commit path=verse.verse.iods.github.out took=584ms apply=543ms append=0s index=41ms other=0s commits=122463

The value written is a 264-byte record (`{delivered, failed, lastPass, lastError}` under a ~566-byte parent of 29 paths), one write per ~1.7s, with a precondition at a sibling (`verse.iods.github.name` present). Other writes in the window are just as slow per commit: the other 42 slow commits (perms nodes, a slack outbox, schedules) averaged 504ms. Over the last ~3.5h of logs the rate is ~2,600 slow commits an hour.

So a commit's apply time does not follow what it writes, as in `fppmhkjsh12kscm9mhn0` (post-rebuild apply) and perhaps `eztp171xh12krdyamdn0` (a one-field commit costs the set).

## Snapshot and compaction never stop

A snapshot is triggered every ~1,200-1,400 commits, and each takes ~25 minutes, with its compaction alongside, so the next is already due when one completes:

    03:55:16 snapshot complete commits=1206 took=1407s
    04:18:31 snapshot complete commits=1212 took=1394s
    04:43:16 snapshot complete commits=1227 took=1484s
    ...
    06:48:03 snapshot complete commits=1389 took=1503s
    07:14:44 snapshot complete commits=1257 took=1600s
    07:14:44 triggering snapshot commitsSinceSnapshot=1353
    07:14:52 starting compaction cutoff=24h0m0s
    07:15:09 compacting original=67999 surviving=67004
    06:48:12 starting compaction ... 07:14:44 compaction complete removed=1094

Each compaction keeps ~98.5% of the inactive log (the 1d cutoff keeps nearly everything) and so rewrites ~620 MB to drop ~1,000 records. Path snapshots are retaken for the hot path itself (`path snapshot created path=verse.verse.iods.github.out.lastPass ... bytes=36`).

On disk: index.regions 5.6G, index.manifest 17M, logA 625M, logB 623M, logA.compact.tmp 199M.

## The question

Why does a commit of a few bytes, at a path with a small value, spend 0.5-0.9s in apply here? Is it apply contending with the snapshot/compaction that is always running, the size of the index, or the per-path snapshot cadence on a path written every couple of seconds? And is a 25-minute snapshot+compaction for a ~620 MB log expected on 2 vCPU?

Verse is cutting its write rate on its side (that record is rewritten on every pass of an outbox loop, six loops at 5s; verse will slow the loops and write only on change), which will mask this rather than explain it.

Context: found while chasing workshop sluggishness on verse staging; the docd logs and profiles are on Scott's machine in the session scratchpad if wanted.